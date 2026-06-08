//go:build js && wasm

package nell

import (
	"encoding/json"
	"fmt"
	"syscall/js"
)

// IndexedDBStore is a persistent Store implementation for the browser,
// backed by IndexedDB.  It implements the nell.Store interface.
type IndexedDBStore struct {
	nodeID string
	db     js.Value
	clock  HLC
}

// NewIndexedDBStore opens (and potentially creates/upgrades) the NellDB
// database in the browser's IndexedDB storage.
func NewIndexedDBStore(nodeID string) (*IndexedDBStore, error) {
	jsIndexedDB := js.Global().Get("indexedDB")
	if jsIndexedDB.IsUndefined() {
		return nil, fmt.Errorf("indexedDB is not available in this environment")
	}

	// Open "NellDB" version 1
	request := jsIndexedDB.Call("open", "NellDB", 1)

	done := make(chan error, 1)
	var db js.Value

	// upgradeHandler is called if the database doesn't exist or version changes.
	upgradeHandler := js.FuncOf(func(this js.Value, args []js.Value) any {
		db := args[0].Get("target").Get("result")

		// Create object store "records" with keyPath: "id"
		objectStore := db.Call("createObjectStore", "records", map[string]any{
			"keyPath": "id",
		})

		// Create non-unique index "clock" on clock.wall_time for range queries.
		// Record struct uses json:"clock" and HLC uses json:"wall_time".
		objectStore.Call("createIndex", "clock", "clock.wall_time", map[string]any{
			"unique": false,
		})

		return nil
	})

	successHandler := js.FuncOf(func(this js.Value, args []js.Value) any {
		db = args[0].Get("target").Get("result")
		done <- nil
		return nil
	})

	errorHandler := js.FuncOf(func(this js.Value, args []js.Value) any {
		errStr := args[0].Get("target").Get("error").Call("toString").String()
		done <- fmt.Errorf("indexedDB open error: %s", errStr)
		return nil
	})

	request.Set("onupgradeneeded", upgradeHandler)
	request.Set("onsuccess", successHandler)
	request.Set("onerror", errorHandler)

	err := <-done

	// Release callbacks as they are no longer needed after the open request finishes.
	upgradeHandler.Release()
	successHandler.Release()
	errorHandler.Release()

	if err != nil {
		return nil, err
	}

	return &IndexedDBStore{
		nodeID: nodeID,
		db:     db,
		clock:  NewHLC(),
	}, nil
}

// Close closes the connection to the IndexedDB database.
func (s *IndexedDBStore) Close() error {
	s.db.Call("close")
	return nil
}

// ── Store Interface Stubs ───────────────────────────────────────────────────

func (s *IndexedDBStore) Put(incoming Record) (bool, Record, error) {
	local, getErr := s.Get(incoming.ID)
	winner := &incoming
	if getErr == nil {
		winner = ResolveConflict(&local, &incoming)
	} else if getErr != ErrRecordNotFound {
		return false, Record{}, getErr
	}

	// Update local clock with incoming clock to stay causally ahead
	s.clock.Update(incoming.Clock)

	// Convert winner to JS object via JSON round-trip
	data, err := json.Marshal(winner)
	if err != nil {
		return false, Record{}, fmt.Errorf("marshal winner: %w", err)
	}
	jsObj := js.Global().Get("JSON").Call("parse", string(data))

	txn := s.db.Call("transaction", []any{"records"}, "readwrite")
	store := txn.Call("objectStore", "records")
	request := store.Call("put", jsObj)

	done := make(chan struct{})
	var putErr error
	onsuccess := js.FuncOf(func(this js.Value, args []js.Value) any {
		close(done)
		return nil
	})
	defer onsuccess.Release()

	onerror := js.FuncOf(func(this js.Value, args []js.Value) any {
		errStr := args[0].Get("target").Get("error").Call("toString").String()
		putErr = fmt.Errorf("indexedDB put error: %s", errStr)
		close(done)
		return nil
	})
	defer onerror.Release()

	request.Set("onsuccess", onsuccess)
	request.Set("onerror", onerror)

	<-done

	if putErr != nil {
		return false, Record{}, putErr
	}

	return winner == &incoming, *winner, nil
}

func (s *IndexedDBStore) Get(id string) (Record, error) {
	txn := s.db.Call("transaction", []any{"records"}, "readonly")
	store := txn.Call("objectStore", "records")
	request := store.Call("get", id)

	done := make(chan struct{})
	var result js.Value
	var err error

	onsuccess := js.FuncOf(func(this js.Value, args []js.Value) any {
		result = args[0].Get("target").Get("result")
		close(done)
		return nil
	})
	defer onsuccess.Release()

	onerror := js.FuncOf(func(this js.Value, args []js.Value) any {
		errStr := args[0].Get("target").Get("error").Call("toString").String()
		err = fmt.Errorf("indexedDB get error: %s", errStr)
		close(done)
		return nil
	})
	defer onerror.Release()

	request.Set("onsuccess", onsuccess)
	request.Set("onerror", onerror)

	<-done

	if err != nil {
		return Record{}, err
	}

	if result.IsNull() || result.IsUndefined() {
		return Record{}, ErrRecordNotFound
	}

	jsonStr := js.Global().Get("JSON").Call("stringify", result).String()
	var rec Record
	if err := json.Unmarshal([]byte(jsonStr), &rec); err != nil {
		return Record{}, fmt.Errorf("failed to unmarshal record: %w", err)
	}

	return rec, nil
}

func (s *IndexedDBStore) Delete(id string) (Record, error) {
	rec, err := s.Get(id)
	if err != nil {
		if err == ErrRecordNotFound {
			rec = Record{ID: id}
		} else {
			return Record{}, err
		}
	}

	rec.Deleted = true
	rec.UpdatedBy = s.nodeID
	rec.Clock = s.clock.Tick()

	_, winner, err := s.Put(rec)
	return winner, err
}

func (s *IndexedDBStore) List() ([]Record, error) {
	txn := s.db.Call("transaction", []any{"records"}, "readonly")
	store := txn.Call("objectStore", "records")
	request := store.Call("getAll")

	done := make(chan struct{})
	var results js.Value
	var err error

	onsuccess := js.FuncOf(func(this js.Value, args []js.Value) any {
		results = args[0].Get("target").Get("result")
		close(done)
		return nil
	})
	defer onsuccess.Release()

	onerror := js.FuncOf(func(this js.Value, args []js.Value) any {
		errStr := args[0].Get("target").Get("error").Call("toString").String()
		err = fmt.Errorf("indexedDB getAll error: %s", errStr)
		close(done)
		return nil
	})
	defer onerror.Release()

	request.Set("onsuccess", onsuccess)
	request.Set("onerror", onerror)

	<-done

	if err != nil {
		return nil, err
	}

	length := results.Get("length").Int()
	var records []Record
	for i := 0; i < length; i++ {
		item := results.Index(i)
		jsonStr := js.Global().Get("JSON").Call("stringify", item).String()
		var rec Record
		if err := json.Unmarshal([]byte(jsonStr), &rec); err != nil {
			return nil, fmt.Errorf("failed to unmarshal record at index %d: %w", i, err)
		}
		if !rec.Deleted {
			records = append(records, rec)
		}
	}

	return records, nil
}

func (s *IndexedDBStore) GetChangesSince(since HLC) ([]Record, error) {
	txn := s.db.Call("transaction", []any{"records"}, "readonly")
	store := txn.Call("objectStore", "records")
	index := store.Call("index", "clock")

	keyRange := js.Global().Get("IDBKeyRange").Call("lowerBound", since.WallTime, false)
	request := index.Call("openCursor", keyRange)

	done := make(chan struct{})
	var records []Record
	var err error
	var closed bool

	var onsuccess js.Func
	onsuccess = js.FuncOf(func(this js.Value, args []js.Value) any {
		if closed {
			return nil
		}
		cursor := args[0].Get("target").Get("result")
		if cursor.IsNull() || cursor.IsUndefined() {
			closed = true
			close(done)
			return nil
		}

		value := cursor.Get("value")
		jsonStr := js.Global().Get("JSON").Call("stringify", value).String()
		var rec Record
		if unmarshalErr := json.Unmarshal([]byte(jsonStr), &rec); unmarshalErr != nil {
			err = fmt.Errorf("unmarshal record: %w", unmarshalErr)
			closed = true
			close(done)
			return nil
		}

		if rec.Clock.GreaterThan(since) {
			records = append(records, rec)
		}

		cursor.Call("continue")
		return nil
	})

	var onerror js.Func
	onerror = js.FuncOf(func(this js.Value, args []js.Value) any {
		if closed {
			return nil
		}
		errStr := args[0].Get("target").Get("error").Call("toString").String()
		err = fmt.Errorf("open cursor: %s", errStr)
		closed = true
		close(done)
		return nil
	})

	request.Set("onsuccess", onsuccess)
	request.Set("onerror", onerror)

	<-done

	onsuccess.Release()
	onerror.Release()

	if err != nil {
		return nil, err
	}

	return records, nil
}

//go:build js && wasm

package nell

import (
	"encoding/json"
	"fmt"
	"runtime"
	"syscall/js"
	"time"
)

// IndexedDBStore is a persistent Store implementation for the browser,
// backed by IndexedDB.  It implements the nell.Store interface.
type IndexedDBStore struct {
	nodeID string
	db     js.Value
	clock  HLC
}

// indexedDBOpenTimeout caps the time we'll wait for an IDB open() to
// settle.  If the user denied storage, IndexedDB is broken by a
// third-party shim, or the database is locked by another tab, the open
// promise may never resolve without one.
const indexedDBOpenTimeout = 5 * time.Second

// NewIndexedDBStore opens (and potentially creates/upgrades) the NellDB
// database in the browser's IndexedDB storage, then resolves a persistent
// nodeID from the "meta" object store.  The nodeID is generated as a fresh
// UUID v4 on first launch and reused on every subsequent launch in the
// same browser profile, so two WASM clients in the same Obsidian vault
// share a nodeID and two clients in different browsers do not.
func NewIndexedDBStore() (*IndexedDBStore, error) {
	jsIndexedDB := js.Global().Get("indexedDB")
	if jsIndexedDB.IsUndefined() {
		return nil, fmt.Errorf("indexedDB is not available in this environment")
	}

	// Open "NellDB" version 2.  v2 adds the "meta" object store for
	// nodeID and other SDK bookkeeping.  v1 had only "records".
	request := jsIndexedDB.Call("open", "NellDB", 2)

	done := make(chan error, 1)
	var db js.Value

	// upgradeHandler is called if the database doesn't exist or version changes.
	upgradeHandler := js.FuncOf(func(this js.Value, args []js.Value) any {
		db := args[0].Get("target").Get("result")

		// Create object store "records" with keyPath: "id"
		records := db.Call("createObjectStore", "records", map[string]any{
			"keyPath": "id",
		})

		// Create non-unique index "clock" on clock.wall_time for range queries.
		// Record struct uses json:"clock" and HLC uses json:"wall_time".
		records.Call("createIndex", "clock", "clock.wall_time", map[string]any{
			"unique": false,
		})

		// "meta" object store holds SDK bookkeeping records keyed by
		// out-of-line string (no keyPath): "node_id" → {value: "uuid"}.
		db.Call("createObjectStore", "meta")

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

	var openErr error
	select {
	case openErr = <-done:
	case <-time.After(indexedDBOpenTimeout):
		upgradeHandler.Release()
		successHandler.Release()
		errorHandler.Release()
		return nil, fmt.Errorf("indexedDB open timed out after %s", indexedDBOpenTimeout)
	}

	// Release callbacks as they are no longer needed after the open request finishes.
	upgradeHandler.Release()
	successHandler.Release()
	errorHandler.Release()

	if openErr != nil {
		return nil, openErr
	}

	nodeID, err := resolveOrCreateNodeID(db)
	if err != nil {
		return nil, fmt.Errorf("resolve persistent nodeID: %w", err)
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

// NodeID returns the persistent node identifier resolved at store creation.
// Stable across reloads (persisted in the "meta" object store) and used as
// Record.UpdatedBy on locally-originated Delete operations.
func (s *IndexedDBStore) NodeID() string {
	return s.nodeID
}

// waitForJS blocks until done is closed.  Plain `<-done` does not yield
// to the JS event loop in Go-WASM, so a pending IndexedDB `onsuccess`
// callback would never fire and the goroutine would sit forever.  The
// fix is to spin on runtime.Gosched() until the callback closes the
// channel.  Gosched is the WASM shim's documented yield point: when
// the scheduler finds no runnable goroutine, the shim schedules a
// setTimeout(0) and returns to JS, which pumps the event loop and
// dispatches the pending callback.  The callback closes the channel,
// the waiting goroutine becomes runnable, and the next Gosched picks
// it up.
func waitForJS(done chan struct{}) {
	for {
		select {
		case <-done:
			return
		default:
			runtime.Gosched()
		}
	}
}

// resolveOrCreateNodeID reads the persistent node ID from the "meta"
// object store, or generates a fresh UUID v4 and persists it on first use.
// The nodeID survives across reloads because it is written to IndexedDB
// before the store is returned.
//
// Two WASM clients in the same browser profile (e.g. the same Obsidian
// vault) share a nodeID; two clients in different browsers do not.  This
// matters for the engine's LWW tie-break, which is deterministic on
// UpdatedBy only when nodeIDs are unique.
func resolveOrCreateNodeID(db js.Value) (string, error) {
	// ── Read existing node_id ─────────────────────────────────────────
	readTxn := db.Call("transaction", []any{"meta"}, "readonly")
	meta := readTxn.Call("objectStore", "meta")
	readReq := meta.Call("get", "node_id")

	readDone := make(chan struct{})
	var result js.Value
	var readErr error

	readOnSuccess := js.FuncOf(func(this js.Value, args []js.Value) any {
		result = args[0].Get("target").Get("result")
		close(readDone)
		return nil
	})
	defer readOnSuccess.Release()

	readOnError := js.FuncOf(func(this js.Value, args []js.Value) any {
		errStr := args[0].Get("target").Get("error").Call("toString").String()
		readErr = fmt.Errorf("read node_id: %s", errStr)
		close(readDone)
		return nil
	})
	defer readOnError.Release()

	readReq.Set("onsuccess", readOnSuccess)
	readReq.Set("onerror", readOnError)
	<-readDone

	if readErr != nil {
		return "", readErr
	}
	if !result.IsNull() && !result.IsUndefined() {
		jsonStr := js.Global().Get("JSON").Call("stringify", result).String()
		var stored struct {
			Value string `json:"value"`
		}
		if err := json.Unmarshal([]byte(jsonStr), &stored); err != nil {
			return "", fmt.Errorf("unmarshal stored node_id: %w", err)
		}
		if stored.Value == "" {
			return "", fmt.Errorf("stored node_id is empty")
		}
		return stored.Value, nil
	}

	// ── No existing nodeID — generate one and persist ────────────────
	newID, err := GenerateUUIDv4()
	if err != nil {
		return "", fmt.Errorf("generate uuid: %w", err)
	}

	writeTxn := db.Call("transaction", []any{"meta"}, "readwrite")
	writeStore := writeTxn.Call("objectStore", "meta")

	jsObj := js.Global().Get("JSON").Call("parse", fmt.Sprintf(`{"value":%q}`, newID))
	writeReq := writeStore.Call("put", jsObj, "node_id")

	writeDone := make(chan struct{})
	var writeErr error

	writeOnSuccess := js.FuncOf(func(this js.Value, args []js.Value) any {
		close(writeDone)
		return nil
	})
	defer writeOnSuccess.Release()

	writeOnError := js.FuncOf(func(this js.Value, args []js.Value) any {
		errStr := args[0].Get("target").Get("error").Call("toString").String()
		writeErr = fmt.Errorf("write node_id: %s", errStr)
		close(writeDone)
		return nil
	})
	defer writeOnError.Release()

	writeReq.Set("onsuccess", writeOnSuccess)
	writeReq.Set("onerror", writeOnError)
	<-writeDone

	if writeErr != nil {
		return "", writeErr
	}
	return newID, nil
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

	waitForJS(done)

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
		js.Global().Get("console").Call("log", "[get] onsuccess")
		result = args[0].Get("target").Get("result")
		close(done)
		return nil
	})
	defer onsuccess.Release()

	onerror := js.FuncOf(func(this js.Value, args []js.Value) any {
		js.Global().Get("console").Call("log", "[get] onerror")
		errStr := args[0].Get("target").Get("error").Call("toString").String()
		err = fmt.Errorf("indexedDB get error: %s", errStr)
		close(done)
		return nil
	})
	defer onerror.Release()

	request.Set("onsuccess", onsuccess)
	request.Set("onerror", onerror)

	waitForJS(done)

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

	waitForJS(done)

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

	waitForJS(done)

	onsuccess.Release()
	onerror.Release()

	if err != nil {
		return nil, err
	}

	return records, nil
}

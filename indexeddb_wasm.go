//go:build js && wasm

package nell

import (
	"encoding/json"
	"fmt"
	"sort"
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

	// Open "NellDB" version 3.  v3 changes "records" to use out-of-line keys
	// (collection:id string) instead of keyPath: "id".
	request := jsIndexedDB.Call("open", "NellDB", 3)

	done := make(chan error, 1)
	var db js.Value

	// upgradeHandler is called if the database doesn't exist or version changes.
	upgradeHandler := js.FuncOf(func(this js.Value, args []js.Value) any {
		db := args[0].Get("target").Get("result")
		oldVersion := args[0].Get("oldVersion").Int()

		if oldVersion < 3 {
			if db.Get("objectStoreNames").Call("contains", "records").Bool() {
				db.Call("deleteObjectStore", "records")
			}
			// Create object store "records" WITHOUT keyPath for manual "collection:id" keys.
			records := db.Call("createObjectStore", "records")

			// Create non-unique index "clock" on clock.wall_time for range queries.
			records.Call("createIndex", "clock", "clock.wall_time", map[string]any{
				"unique": false,
			})
		}

		if oldVersion < 2 {
			// "meta" object store holds SDK bookkeeping records keyed by
			// out-of-line string (no keyPath): "node_id" → {value: "uuid"}.
			if !db.Get("objectStoreNames").Call("contains", "meta").Bool() {
				db.Call("createObjectStore", "meta")
			}
		}

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

// waitForJS blocks until done is closed.  When called from a goroutine
// that was triggered by a JS Promise (as in nellPut/nellGet), blocking
// on the channel allows the Go scheduler to yield to the JS event loop,
// enabling IndexedDB callbacks to fire.
func waitForJS(done chan struct{}) {
	<-done
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
	waitForJS(readDone)

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
	waitForJS(writeDone)

	if writeErr != nil {
		return "", writeErr
	}
	return newID, nil
}

// ── Store Interface implementation ──────────────────────────────────────────

func (s *IndexedDBStore) Put(incoming Record) (bool, Record, error) {
	if incoming.Collection == "" {
		incoming.Collection = DefaultCollection
	}

	local, getErr := s.Get(incoming.Collection, incoming.ID)
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

	key := winner.Collection + ":" + winner.ID
	request := store.Call("put", jsObj, key)

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

func (s *IndexedDBStore) Get(collection, id string) (Record, error) {
	if collection == "" {
		collection = DefaultCollection
	}

	txn := s.db.Call("transaction", []any{"records"}, "readonly")
	store := txn.Call("objectStore", "records")

	key := collection + ":" + id
	request := store.Call("get", key)

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

func (s *IndexedDBStore) Delete(collection, id string) (Record, error) {
	if collection == "" {
		collection = DefaultCollection
	}

	rec, err := s.Get(collection, id)
	if err != nil {
		if err == ErrRecordNotFound {
			rec = Record{Collection: collection, ID: id}
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

func (s *IndexedDBStore) List(collection string) ([]Record, error) {
	if collection == "" {
		collection = DefaultCollection
	}

	txn := s.db.Call("transaction", []any{"records"}, "readonly")
	store := txn.Call("objectStore", "records")

	// Optimize: Use a key range to fetch only records for this collection.
	keyRange := js.Global().Get("IDBKeyRange").Call("bound", collection+":", collection+":\uffff")
	request := store.Call("getAll", keyRange)

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

func (s *IndexedDBStore) Query(q Query) ([]Record, error) {
	return s.List(q.Collection)
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

	var onsuccess js.Func
	onsuccess = js.FuncOf(func(this js.Value, args []js.Value) any {
		cursor := args[0].Get("target").Get("result")
		if cursor.IsNull() || cursor.IsUndefined() {
			close(done)
			return nil
		}

		value := cursor.Get("value")
		jsonStr := js.Global().Get("JSON").Call("stringify", value).String()
		var rec Record
		if unmarshalErr := json.Unmarshal([]byte(jsonStr), &rec); unmarshalErr != nil {
			err = fmt.Errorf("unmarshal record: %w", unmarshalErr)
			close(done)
			return nil
		}

		if rec.Clock.GreaterThan(since) {
			records = append(records, rec)
		}

		cursor.Call("continue")
		return nil
	})
	defer onsuccess.Release()

	var onerror js.Func
	onerror = js.FuncOf(func(this js.Value, args []js.Value) any {
		errStr := args[0].Get("target").Get("error").Call("toString").String()
		err = fmt.Errorf("open cursor: %s", errStr)
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

	return records, nil
}

// SearchSimilar finds the top-K records most similar to the given vector
// using Cosine Similarity.
func (s *IndexedDBStore) SearchSimilar(collection string, queryVector []float32, limit int) ([]Record, error) {
	if collection == "" {
		collection = DefaultCollection
	}

	all, err := s.List(collection)
	if err != nil {
		return nil, err
	}

	type scoredRecord struct {
		rec   Record
		score float32
	}

	var allScored []scoredRecord
	for _, r := range all {
		if !r.Deleted && r.Type == TypeVector && len(r.Vector) > 0 {
			score := CosineSimilarity(queryVector, r.Vector)
			allScored = append(allScored, scoredRecord{rec: r, score: score})
		}
	}

	sort.Slice(allScored, func(i, j int) bool {
		if allScored[i].score == allScored[j].score {
			return allScored[i].rec.ID < allScored[j].rec.ID
		}
		return allScored[i].score > allScored[j].score
	})

	if limit > 0 && len(allScored) > limit {
		allScored = allScored[:limit]
	}

	out := make([]Record, len(allScored))
	for i, sr := range allScored {
		out[i] = sr.rec
	}

	return out, nil
}

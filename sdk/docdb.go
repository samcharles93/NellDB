package sdk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"runtime"
	"sync"
	"time"

	"github.com/samcharles93/NellDB"
)

// DocDB is a document database backed by any nell.Store.  It is safe for
// concurrent use.
//
// # Wire format
//
// Every document round-trips through the underlying nell.Record as:
//
//	nell.Record {
//	    ID        = doc._id
//	    Deleted   = doc._deleted
//	    Payload   = JSON({"_rev": rev, ...userFields})
//	    Clock     = HLC (drives replication ordering)
//	    UpdatedBy = nodeID
//	    Type      = nell.TypeText  (wire-only; ignored by the SDK)
//	}
//
// _rev lives inside the payload, not the record, so a peer's SDK (or any
// future versioned client) sees the same shape and revs survive the wire.
// The in-memory revs map is a cache rebuilt on startup by scanning all
// records; the payload is the source of truth.
//
// Typical use:
//
//	db := sdk.New(nell.NewMemoryStore("node-a"), "node-a")
//	rev, err := db.Put(ctx, sdk.Doc{sdk.FieldID: "x", "name": "X"})
//	got, err := db.Get(ctx, "x")
//	rows, err := db.AllDocs(ctx, sdk.DocRange{IncludeDocs: true})
type DocDB struct {
	store      nell.Store
	nodeID     string
	collection string

	mu     sync.RWMutex
	revs   map[string]string    // id -> current rev, cache of payload truth
	vector nell.KnowledgeVector // per-peer "highest clock seen" (replication state)

	// lastSeenClock is the highest Clock we have ever observed from a peer
	// during replication.  Persisted as a meta:clock doc so the next session
	// resumes incremental pulls instead of starting from zero.
	lastSeenClock nell.HLC

	subs *changesHub
}

// New wraps a nell.Store as a DocDB.  nodeID is the writer identity stamped
// on every local record (used by the engine's LWW tie-break and surfaced
// as UpdatedBy to peers).
//
// On construction, DocDB scans the store to rebuild the in-memory _rev index
// and the replication clock.  O(n) over local records; fine for in-memory and
// the replay path of LogStore.
func New(store nell.Store, nodeID string, collection string) *DocDB {
	if collection == "" {
		collection = nell.DefaultCollection
	}
	d := &DocDB{
		store:      store,
		nodeID:     nodeID,
		collection: collection,
		revs:       make(map[string]string),
		subs:       newChangesHub(),
	}
	if kv, ok := d.readMetaVector(); ok {
		d.vector = kv
	}
	d.reindex()
	return d
}

// reindex rebuilds the rev cache and replication clock from the logstore.
// Called on New() and after a wholesale import.  Idempotent.
func (d *DocDB) reindex() {
	all, err := d.store.List(d.collection)
	if err != nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.revs = make(map[string]string, len(all))
	var maxClock nell.HLC
	for _, rec := range all {
		if rev, ok := readRev(rec); ok {
			d.revs[rec.ID] = rev
		} else {
			// Imported / legacy records: assign a stable placeholder so the
			// MVCC chain still works.  Conflicts are impossible for these.
			d.revs[rec.ID] = "1-imported"
		}
		if rec.Clock.GreaterThan(maxClock) {
			maxClock = rec.Clock
		}
	}
	// Persisted clock takes precedence: it tracks what we have *seen* from
	// peers, not what we have *stored* locally.
	if mc, ok := d.readMetaClock(); ok && mc.GreaterThan(maxClock) {
		maxClock = mc
	}
	d.lastSeenClock = maxClock
}

// NodeID returns the writer identity this database stamps on local records.
func (d *DocDB) NodeID() string { return d.nodeID }

// Store exposes the underlying engine.  Provided for tests and for advanced
// callers that need to drive nell.Store directly; the SDK is the supported
// public API.
func (d *DocDB) Store() nell.Store { return d.store }

// LastSeenClock returns the highest clock the database has ever observed
// (locally or from a peer).  Used by the replicator to issue incremental
// pulls after a restart.
func (d *DocDB) LastSeenClock() nell.HLC {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.lastSeenClock
}

// ── CRUD ─────────────────────────────────────────────────────────────────────

// Put inserts or updates a document.
//
// If doc._rev is set it must match the current local revision, otherwise
// ErrConflict is returned.  If doc._rev is empty the SDK continues the chain
// from the current local rev (so read-modify-write loops work without
// forcing the caller to track revs explicitly).
//
// Returns the new revision token.  Also stamps it into doc._rev.
func (d *DocDB) Put(ctx context.Context, doc Doc) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	id, _ := doc[FieldID].(string)
	if id == "" {
		return "", ErrMissingID
	}

	d.mu.Lock()
	curRev, exists := d.revs[id]
	d.mu.Unlock()

	incomingRev, _ := doc[FieldRev].(string)
	if incomingRev != "" && curRev != "" && incomingRev != curRev {
		return "", ErrConflict
	}
	if incomingRev != "" && !exists {
		return "", ErrConflict
	}
	baseRev := incomingRev
	if baseRev == "" && exists {
		baseRev = curRev
	}

	body := splitDoc(doc)
	deleted := isDeleted(doc)
	raw, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("sdk: marshal: %w", err)
	}
	newRev := genRev(baseRev, raw)

	// Re-stamp the new rev into the body so the wire format carries it.
	body[FieldRev] = newRev
	final, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("sdk: marshal final: %w", err)
	}

	recType := nell.TypeText
	if tStr, ok := doc[FieldType].(string); ok {
		recType = tStr
	}

	var vector []float32
	if vec, ok := doc[FieldVector].([]float32); ok {
		vector = vec
	} else if vecIntf, ok := doc[FieldVector].([]any); ok {
		// When parsing from JSON, numbers might come as float64 (via interface{})
		vector = make([]float32, len(vecIntf))
		for i, v := range vecIntf {
			if f, ok := v.(float64); ok {
				vector[i] = float32(f)
			}
		}
	}

	rec := nell.Record{
		Collection: d.collection,
		ID:         id,
		Type:       recType,
		Payload:    final,
		Vector:     vector,
		UpdatedBy:  d.nodeID,
		Deleted:    deleted,
	}
	// HLC: merge with any existing clock so concurrent local + remote writes
	// converge to the latest logical time.  Use the local clock as the
	// authoritative time on this node.
	clk := nell.NewHLC()
	if existing, err := d.store.Get(d.collection, id); err == nil {
		clk.Update(existing.Clock)
	}
	rec.Clock = clk.Tick()

	if _, _, err := d.store.Put(rec); err != nil {
		return "", fmt.Errorf("sdk: store put: %w", err)
	}

	d.mu.Lock()
	d.revs[id] = newRev
	d.mu.Unlock()
	doc[FieldRev] = newRev
	d.observeVector(d.nodeID, rec.Clock)

	d.subs.broadcast(Change{ID: id, Rev: newRev, Deleted: deleted, Doc: cloneDoc(doc)})
	return newRev, nil
}

// Get fetches a document by _id.  Returns ErrNotFound for missing or
// tombstoned documents.
func (d *DocDB) Get(ctx context.Context, id string) (Doc, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	rec, err := d.store.Get(d.collection, id)
	if err != nil {
		if isNotFoundErr(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if rec.Deleted {
		return nil, ErrNotFound
	}
	return joinDoc(id, rec), nil
}

// Remove tombstones a document.  Accepts either an _id string or a Doc with
// a _rev (for read-modify-write).  Idempotent on missing docs.
func (d *DocDB) Remove(ctx context.Context, idOrDoc any) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	var id, wantRev string
	switch v := idOrDoc.(type) {
	case string:
		id = v
	case Doc:
		id, _ = v[FieldID].(string)
		wantRev, _ = v[FieldRev].(string)
	default:
		return "", fmt.Errorf("sdk: Remove expects string id or Doc, got %T", idOrDoc)
	}
	if id == "" {
		return "", ErrMissingID
	}

	d.mu.Lock()
	curRev, exists := d.revs[id]
	d.mu.Unlock()

	if wantRev != "" && curRev != "" && wantRev != curRev {
		return "", ErrConflict
	}
	baseRev := wantRev
	if baseRev == "" && exists {
		baseRev = curRev
	}

	rec, err := d.store.Get(d.collection, id)
	if err != nil {
		if isNotFoundErr(err) {
			rec = nell.Record{Collection: d.collection, ID: id, Type: nell.TypeText, UpdatedBy: d.nodeID, Clock: nell.NewHLC()}
		} else {
			return "", err
		}
	}
	rec.Deleted = true
	rec.UpdatedBy = d.nodeID
	clk := nell.NewHLC()
	clk.Update(rec.Clock)
	rec.Clock = clk.Tick()

	if _, _, err := d.store.Put(rec); err != nil {
		return "", fmt.Errorf("sdk: store put (tombstone): %w", err)
	}
	newRev := genRev(baseRev, nil)
	d.mu.Lock()
	d.revs[id] = newRev
	d.mu.Unlock()
	d.observeVector(d.nodeID, rec.Clock)
	d.subs.broadcast(Change{ID: id, Rev: newRev, Deleted: true, Doc: Doc{FieldID: id, FieldRev: newRev, FieldDeleted: true}})
	return newRev, nil
}

// PutMany writes a batch of documents atomically: any error rolls back the
// in-memory cache so a partial failure doesn't leave a half-applied state.
// The underlying nell.Store decides what "atomic" means — MemoryStore and
// LogStore apply each Put sequentially, so a crash mid-batch leaves the
// earlier writes applied.  Callers that need strict atomicity should use a
// backend that supports transactions.
func (d *DocDB) PutMany(ctx context.Context, docs []Doc) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Snapshot existing revs so we can roll back on error.
	d.mu.RLock()
	prev := make([]bulkSnap, len(docs))
	for i, doc := range docs {
		id, _ := doc[FieldID].(string)
		r, ok := d.revs[id]
		prev[i] = bulkSnap{id: id, rev: r, existed: ok}
	}
	d.mu.RUnlock()

	results := make([]string, len(docs))
	for i, doc := range docs {
		rev, err := d.Put(ctx, doc)
		if err != nil {
			d.rollback(prev)
			return nil, fmt.Errorf("sdk: PutMany[%d] %q: %w", i, doc[FieldID], err)
		}
		results[i] = rev
	}
	return results, nil
}

func (d *DocDB) rollback(prev []bulkSnap) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, s := range prev {
		if s.existed {
			d.revs[s.id] = s.rev
		} else {
			delete(d.revs, s.id)
		}
	}
}

// GetMany fetches multiple documents by id in one call.  Missing or deleted
// ids are silently skipped; the returned map only contains the present ones.
func (d *DocDB) GetMany(ctx context.Context, ids []string) (map[string]Doc, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	out := make(map[string]Doc, len(ids))
	for _, id := range ids {
		doc, err := d.Get(ctx, id)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				continue
			}
			return nil, err
		}
		out[id] = doc
	}
	return out, nil
}

// AllDocs returns rows for the given range.  The result is a snapshot —
// concurrent writes may produce a newer state.
func (d *DocDB) AllDocs(ctx context.Context, rng DocRange) (AllDocsResult, error) {
	if err := ctx.Err(); err != nil {
		return AllDocsResult{}, err
	}

	if len(rng.Keys) > 0 {
		rows := make([]DocRow, 0, len(rng.Keys))
		for _, id := range rng.Keys {
			if !keyInRange(id, rng.StartKey, rng.EndKey, rng.InclusiveEnd) {
				continue
			}
			rec, err := d.store.Get(d.collection, id)
			if err != nil || rec.Deleted {
				continue
			}
			rows = append(rows, d.makeRow(id, rec, rng.IncludeDocs))
		}
		return AllDocsResult{TotalRows: len(rows), Offset: 0, Rows: rows}, nil
	}

	all, err := d.store.List(d.collection)
	if err != nil {
		return AllDocsResult{}, err
	}

	// ── Parallel Range Scan ───────────────────────────────────────────
	numWorkers := min(runtime.NumCPU(), 12)
	if len(all) < 10000 { // Don't overhead with parallelism for small sets
		numWorkers = 1
	}

	results := make(chan []DocRow, numWorkers)
	chunkSize := (len(all) + numWorkers - 1) / numWorkers
	var wg sync.WaitGroup

	for w := range numWorkers {
		start := w * chunkSize
		if start >= len(all) {
			break
		}
		end := min(start+chunkSize, len(all))

		wg.Add(1)
		go func(chunk []nell.Record) {
			defer wg.Done()
			rows := make([]DocRow, 0, len(chunk)/10) // heuristic initial capacity
			for _, rec := range chunk {
				if rec.Deleted {
					continue
				}
				if !keyInRange(rec.ID, rng.StartKey, rng.EndKey, rng.InclusiveEnd) {
					continue
				}
				rows = append(rows, d.makeRow(rec.ID, rec, rng.IncludeDocs))
			}
			results <- rows
		}(all[start:end])
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	var finalRows []DocRow
	for rows := range results {
		finalRows = append(finalRows, rows...)
	}

	totalFound := len(finalRows)
	if rng.Skip > 0 {
		if rng.Skip >= len(finalRows) {
			finalRows = nil
		} else {
			finalRows = finalRows[rng.Skip:]
		}
	}
	if rng.Limit > 0 && len(finalRows) > rng.Limit {
		finalRows = finalRows[:rng.Limit]
	}

	return AllDocsResult{TotalRows: totalFound, Rows: finalRows}, nil
}

// SearchSimilar performs a vector similarity search (Cosine Similarity) against
// all records of type "vector" in the collection. Returns the top-K matching
// documents.
func (d *DocDB) SearchSimilar(ctx context.Context, vector []float32, limit int) ([]Doc, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if limit <= 0 {
		limit = 10 // Default limit
	}

	records, err := d.store.SearchSimilar(d.collection, vector, limit)
	if err != nil {
		return nil, err
	}

	docs := make([]Doc, 0, len(records))
	for _, rec := range records {
		doc := joinDoc(rec.ID, rec)

		d.mu.RLock()
		if rev, ok := d.revs[rec.ID]; ok {
			doc[FieldRev] = rev
		}
		d.mu.RUnlock()

		docs = append(docs, doc)
	}

	return docs, nil
}

func (d *DocDB) makeRow(id string, rec nell.Record, includeDoc bool) DocRow {
	d.mu.RLock()
	rev, ok := d.revs[id]
	d.mu.RUnlock()
	if !ok {
		rev, _ = readRev(rec)
	}
	if rev == "" {
		rev = "1-imported"
	}
	row := DocRow{ID: id, Key: id}
	row.Value.Rev = rev
	if includeDoc {
		row.Doc = joinDoc(id, rec)
	}
	return row
}

// listAll returns every record in the collection, including tombstones.
// Unlike store.List, this is used by the Replicator to push deletions to peers.
func (d *DocDB) listAll() ([]nell.Record, error) {
	all, err := d.store.GetChangesSince(nell.HLC{})
	if err != nil {
		return nil, err
	}
	filtered := all[:0]
	for _, rec := range all {
		if rec.Collection == d.collection {
			filtered = append(filtered, rec)
		}
	}
	return filtered, nil
}

// ── Info / lifecycle ─────────────────────────────────────────────────────────

// Info is a snapshot of the database's bookkeeping.  Cheap to compute.
//
// TombstoneCount is always 0 — the underlying nell.Store hides tombstones
// from List() (only Get() can see them), so the SDK can't cheaply count
// them.  Use the changes feed or scan the underlying store directly if
// you need the count.
type Info struct {
	NodeID         string    `json:"node_id"`
	DocCount       int       `json:"doc_count"`
	TombstoneCount int       `json:"tombstone_count"`
	LastSeenClock  nell.HLC  `json:"last_seen_clock"`
	Built          time.Time `json:"built"`
}

// Info returns a snapshot of the database's bookkeeping.
func (d *DocDB) Info() Info {
	all, _ := d.store.List(d.collection)
	live := 0
	for _, r := range all {
		if isInternalID(r.ID) {
			continue
		}
		live++
	}
	return Info{
		NodeID:         d.NodeID(),
		DocCount:       live,
		TombstoneCount: 0, // see comment on Info
		LastSeenClock:  d.LastSeenClock(),
		Built:          time.Now(),
	}
}

// Destroy tombstones every record and clears the in-memory bookkeeping
// (revs, lastSeenClock, vector).  Each tombstone is published through
// Remove so observers of the changes feed see the deletions.
//
// The underlying store is left open — callers should call Store().Close()
// if they want the engine torn down too.  Space on disk is not reclaimed;
// that requires a compaction pass on the LogStore (TODO v0.2).
func (d *DocDB) Destroy(ctx context.Context) error {
	all, err := d.store.List(d.collection)
	if err != nil {
		return err
	}
	for _, rec := range all {
		if isInternalID(rec.ID) {
			continue
		}
		if _, err := d.store.Get(d.collection, rec.ID); err != nil {
			continue
		}
		if _, err := d.Remove(ctx, rec.ID); err != nil {
			return err
		}
	}
	d.mu.Lock()
	d.revs = make(map[string]string)
	d.lastSeenClock = nell.HLC{}
	d.vector = nil
	d.mu.Unlock()
	return nil
}

// ── Helpers ──────────────────────────────────────────────────────────────────

// bulkSnap is a snapshot of a DocDB row taken at the start of a PutMany so
// the cache can be restored if a later write in the batch fails.
type bulkSnap struct {
	id, rev string
	existed bool
}

// splitDoc returns the document minus the record-level fields _id and
// _deleted.  _rev is kept (it lives inside the payload) so the wire format
// round-trips the rev token.
func splitDoc(doc Doc) map[string]any {
	body := make(map[string]any, len(doc))
	for k, v := range doc {
		if k == FieldID || k == FieldDeleted || k == FieldType || k == FieldVector {
			continue
		}
		body[k] = v
	}
	return body
}

func isDeleted(doc Doc) bool {
	if b, ok := doc[FieldDeleted].(bool); ok {
		return b
	}
	return false
}

// joinDoc rebuilds a Doc from a stored payload.  The _rev is taken from the
// payload body (where the wire carries it), falling back to "1-imported" for
// legacy records that pre-date the rev-in-payload convention.
func joinDoc(id string, rec nell.Record) Doc {
	out := Doc{FieldID: id}

	if rec.Type != "" && rec.Type != nell.TypeText {
		out[FieldType] = rec.Type
	}
	if len(rec.Vector) > 0 {
		out[FieldVector] = rec.Vector
	}

	if len(rec.Payload) == 0 {
		return out
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Payload, &body); err != nil {
		out["_payload_b64"] = rec.Payload
		return out
	}
	maps.Copy(out, body)
	if _, ok := out[FieldRev].(string); !ok {
		out[FieldRev] = "1-imported"
	}
	return out
}

// readRev extracts the _rev from a nell.Record's payload.  Returns "" and
// false for records that pre-date the rev-in-payload convention.
func readRev(rec nell.Record) (string, bool) {
	if len(rec.Payload) == 0 {
		return "", false
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Payload, &body); err != nil {
		return "", false
	}
	v, ok := body[FieldRev].(string)
	return v, ok
}

func cloneDoc(d Doc) Doc {
	out := make(Doc, len(d))
	maps.Copy(out, d)
	return out
}

func keyInRange(key, start, end string, inclusiveEnd bool) bool {
	if start != "" && key < start {
		return false
	}
	if end == "" {
		return true
	}
	if inclusiveEnd {
		return key <= end
	}
	return key < end
}

func isNotFoundErr(err error) bool {
	return errors.Is(err, nell.ErrRecordNotFound)
}

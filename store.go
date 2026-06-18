package nell

import (
	"encoding/binary"
	"fmt"
	"maps"
	"runtime"
	"sort"
	"sync"
)

// ── Store Interface ───────────────────────────────────────────────────────────

// Query is a placeholder for future query capabilities.
type Query struct {
	Collection string
}

// Store is the abstract storage layer.  Every backend — in-memory, bbolt,
// IndexedDB — implements this interface.  The sync engine and conflict
// resolver operate on Store, never on a concrete backend.
type Store interface {
	Put(incoming Record) (accepted bool, current Record, err error)
	Get(collection, id string) (Record, error)
	Delete(collection, id string) (Record, error)
	List(collection string) ([]Record, error)
	ListAll(collection string) ([]Record, error) // includes tombstones
	Query(q Query) ([]Record, error)
	GetChangesSince(clock HLC) ([]Record, error)
	SearchSimilar(collection string, vector []float32, limit int) ([]Record, error)
	// NodeID returns the stable identifier of the local node.  It is
	// stamped on every locally-originated record (Delete, PutLocal) and
	// must be unique per node so the engine's LWW tie-break on
	// UpdatedBy is deterministic.  Backends persist it where possible
	// (LogStore on disk, IndexedDBStore in the "meta" object store) so
	// the same nodeID survives restarts.
	NodeID() string
	Close() error
}

// ── Knowledge Vector ──────────────────────────────────────────────────────────

// KnowledgeVector tracks the highest HLC the local node has seen from each
// remote node.  It is the core data structure for anti-entropy sync.
type KnowledgeVector map[string]HLC

// Update advances the vector entry for nodeID if clock is newer.
func (kv KnowledgeVector) Update(nodeID string, clock HLC) {
	if existing, ok := kv[nodeID]; !ok || clock.GreaterThan(existing) {
		kv[nodeID] = clock
	}
}

// MarshalBinary encodes the KnowledgeVector into a compact binary format.
// [2 bytes: number of entries]
// For each entry:
//
//	[2 bytes: nodeID len]
//	[N bytes: nodeID string]
//	[12 bytes: HLC clock]
func (kv KnowledgeVector) MarshalBinary() ([]byte, error) {
	size := 2
	for nodeID := range kv {
		size += 2 + len(nodeID) + HLCSize
	}

	b := make([]byte, size)
	binary.BigEndian.PutUint16(b[0:2], uint16(len(kv)))

	off := 2
	for nodeID, clock := range kv {
		binary.BigEndian.PutUint16(b[off:off+2], uint16(len(nodeID)))
		off += 2
		copy(b[off:off+len(nodeID)], nodeID)
		off += len(nodeID)
		clock.EncodeBinary(b[off : off+HLCSize])
		off += HLCSize
	}
	return b, nil
}

// UnmarshalBinary decodes the KnowledgeVector from a compact binary format.
func (kv KnowledgeVector) UnmarshalBinary(b []byte) error {
	if len(b) < 2 {
		return fmt.Errorf("kv: binary too short")
	}
	count := int(binary.BigEndian.Uint16(b[0:2]))
	off := 2

	for i := range count {
		if len(b) < off+2 {
			return fmt.Errorf("kv: truncated at entry %d", i)
		}
		idLen := int(binary.BigEndian.Uint16(b[off : off+2]))
		off += 2
		if len(b) < off+idLen+HLCSize {
			return fmt.Errorf("kv: truncated at entry %d data", i)
		}
		nodeID := string(b[off : off+idLen])
		off += idLen
		var clock HLC
		clock.DecodeBinary(b[off : off+HLCSize])
		off += HLCSize
		kv[nodeID] = clock
	}
	return nil
}

// ── Conflict Resolution ──────────────────────────────────────────────────────

// ResolveConflict applies Last-Write-Wins (LWW) with deterministic tie-breaking.
// Returns the winning Record.  Must be called on every Put that collides on ID.
func ResolveConflict(local, incoming *Record) *Record {
	if incoming.Clock.GreaterThan(local.Clock) {
		return incoming
	}
	if local.Clock.GreaterThan(incoming.Clock) {
		return local
	}
	// Clocks equal → deterministic lexical tie-break on node ID
	if incoming.UpdatedBy > local.UpdatedBy {
		return incoming
	}
	return local
}

// ── MemoryStore ───────────────────────────────────────────────────────────────

// MemoryStore is a thread-safe in-memory implementation of Store, backed by a
// Go map. It is the default backend used in the PoC server, WASM client, and
// all tests.
type MemoryStore struct {
	mu      sync.RWMutex
	nodeID  string
	clock   HLC
	records map[string]Record
	kv      KnowledgeVector

	// colIndex maps collection name → set of record keys in that collection.
	// It backs List/ListAll so they are O(collection size) instead of a full
	// scan.  Maintained incrementally on every write.
	colIndex map[string]map[string]struct{}

	// changesIdx is a lazily-rebuilt slice of (clock, key) pairs in ascending
	// HLC order, backing GetChangesSince so it is O(log n + k) instead of a
	// full scan.  Writes set changesDirty; the next query rebuilds if needed.
	changesIdx   []clockKey
	changesDirty bool
}

// clockKey is a single entry in the changesIdx: the current winning HLC for a
// record key, used to binary-search the GetChangesSince lower bound.
type clockKey struct {
	clock HLC
	key   string
}

// NewMemoryStore returns an initialised MemoryStore with the given node
// identifier (used as Record.UpdatedBy on every write).
func NewMemoryStore(nodeID string) *MemoryStore {
	return &MemoryStore{
		nodeID:        nodeID,
		clock:         NewHLC(),
		records:       make(map[string]Record),
		kv:            make(KnowledgeVector),
		colIndex:      make(map[string]map[string]struct{}),
		changesDirty:  true,
	}
}

// NodeID returns the store's node identifier.
func (s *MemoryStore) NodeID() string { return s.nodeID }

// KnowledgeVector returns a copy of the local knowledge vector.
func (s *MemoryStore) KnowledgeVector() KnowledgeVector {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cp := make(KnowledgeVector, len(s.kv))
	maps.Copy(cp, s.kv)
	return cp
}

// indexKey adds key to the collection index for the given collection.  Called
// under the write lock by every write path.  O(1) amortized.
func (s *MemoryStore) indexKey(key, collection string) {
	set, ok := s.colIndex[collection]
	if !ok {
		set = make(map[string]struct{})
		s.colIndex[collection] = set
	}
	set[key] = struct{}{}
}

// markChangesDirty flags the HLC index as stale so the next GetChangesSince
// rebuilds it.  Called under the write lock by every write path.
func (s *MemoryStore) markChangesDirty() {
	s.changesDirty = true
}

// rebuildChangesIndex rebuilds the sorted (clock, key) slice from the current
// records map.  Called under the read lock.
func (s *MemoryStore) rebuildChangesIndex() {
	idx := make([]clockKey, 0, len(s.records))
	for key, rec := range s.records {
		idx = append(idx, clockKey{clock: rec.Clock, key: key})
	}
	sort.Slice(idx, func(i, j int) bool {
		a, b := idx[i].clock, idx[j].clock
		if a.WallTime != b.WallTime {
			return a.WallTime < b.WallTime
		}
		if a.Counter != b.Counter {
			return a.Counter < b.Counter
		}
		return idx[i].key < idx[j].key
	})
	s.changesIdx = idx
	s.changesDirty = false
}

// Put inserts or updates a record using LWW conflict resolution.
func (s *MemoryStore) Put(incoming Record) (bool, Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if incoming.Collection == "" {
		incoming.Collection = DefaultCollection
	}

	// Update local HLC with incoming clock
	s.clock.Update(incoming.Clock)

	key := incoming.Collection + ":" + incoming.ID
	local, exists := s.records[key]
	if !exists {
		s.records[key] = incoming
		s.indexKey(key, incoming.Collection)
		s.kv.Update(incoming.UpdatedBy, incoming.Clock)
		s.markChangesDirty()
		return true, incoming, nil
	}

	winner := ResolveConflict(&local, &incoming)
	s.records[key] = *winner
	s.kv.Update(winner.UpdatedBy, winner.Clock)
	s.markChangesDirty()
	return winner == &incoming, *winner, nil
}

// PutLocal creates or updates a record with a fresh local HLC clock.  This is
// the method the application calls for local writes (as opposed to Put, which
// is used during sync ingestion).
func (s *MemoryStore) PutLocal(rec *Record) (Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if rec.Collection == "" {
		rec.Collection = DefaultCollection
	}

	rec.Clock = s.clock.Tick()
	rec.UpdatedBy = s.nodeID

	key := rec.Collection + ":" + rec.ID
	_, existed := s.records[key]
	s.records[key] = *rec
	if !existed {
		s.indexKey(key, rec.Collection)
	}
	s.kv.Update(s.nodeID, rec.Clock)
	s.markChangesDirty()
	return *rec, nil
}

// Get returns the record with the given ID in the specified collection, or an error if not found.
func (s *MemoryStore) Get(collection, id string) (Record, error) {
	if collection == "" {
		collection = DefaultCollection
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	key := collection + ":" + id
	rec, ok := s.records[key]
	if !ok {
		return Record{}, fmt.Errorf("record %q in collection %q: %w", id, collection, ErrRecordNotFound)
	}
	return rec, nil
}

// Delete tombstones a record by writing a Deleted=true entry with a fresh
// local clock.
func (s *MemoryStore) Delete(collection, id string) (Record, error) {
	if collection == "" {
		collection = DefaultCollection
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	key := collection + ":" + id
	rec, ok := s.records[key]
	if !ok {
		rec = Record{Collection: collection, ID: id}
	}
	rec.Clock = s.clock.Tick()
	rec.UpdatedBy = s.nodeID
	rec.Deleted = true
	// Keep the existing type so the tombstone travels with the right discriminator.

	s.records[key] = rec
	if !ok {
		s.indexKey(key, collection)
	}
	s.kv.Update(s.nodeID, rec.Clock)
	s.markChangesDirty()
	return rec, nil
}

// List returns all non-deleted records in the specified collection.
func (s *MemoryStore) List(collection string) ([]Record, error) {
	if collection == "" {
		collection = DefaultCollection
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	set, ok := s.colIndex[collection]
	if !ok {
		return nil, nil
	}
	out := make([]Record, 0, len(set))
	for key := range set {
		r := s.records[key]
		if !r.Deleted {
			out = append(out, r)
		}
	}
	return out, nil
}

// ListAll returns all records in the collection, including tombstones.
// Used by anti-entropy and replication to propagate deletions.
func (s *MemoryStore) ListAll(collection string) ([]Record, error) {
	if collection == "" {
		collection = DefaultCollection
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	set, ok := s.colIndex[collection]
	if !ok {
		return nil, nil
	}
	out := make([]Record, 0, len(set))
	for key := range set {
		out = append(out, s.records[key])
	}
	return out, nil
}

// Query returns all non-deleted records in the specified collection (basic stub).
func (s *MemoryStore) Query(q Query) ([]Record, error) {
	return s.List(q.Collection)
}

// GetChangesSince returns every record whose clock is strictly greater than
// the given lower bound.  This is used by the sync protocol to compute deltas.
func (s *MemoryStore) GetChangesSince(since HLC) ([]Record, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.changesDirty {
		s.rebuildChangesIndex()
	}

	idx := sort.Search(len(s.changesIdx), func(i int) bool {
		ck := s.changesIdx[i].clock
		return ck.WallTime > since.WallTime ||
			(ck.WallTime == since.WallTime && ck.Counter > since.Counter)
	})

	out := make([]Record, 0, len(s.changesIdx)-idx)
	for ; idx < len(s.changesIdx); idx++ {
		rec, ok := s.records[s.changesIdx[idx].key]
		if !ok {
			continue
		}
		out = append(out, rec)
	}
	return out, nil
}

// SearchSimilar finds the top-K records most similar to the given vector
// using Cosine Similarity. It leverages 1BRC-style parallelism to saturate
// CPU cores during the linear scan.
func (s *MemoryStore) SearchSimilar(collection string, queryVector []float32, limit int) ([]Record, error) {
	if collection == "" {
		collection = DefaultCollection
	}

	s.mu.RLock()
	var candidates []Record
	for _, r := range s.records {
		if r.Collection == collection && !r.Deleted && r.Type == TypeVector && len(r.Vector) > 0 {
			candidates = append(candidates, r)
		}
	}
	s.mu.RUnlock()

	if len(candidates) == 0 {
		return nil, nil
	}

	type scoredRecord struct {
		rec   Record
		score float32
	}

	numWorkers := runtime.NumCPU()
	if len(candidates) < 1000 {
		numWorkers = 1
	}

	results := make(chan []scoredRecord, numWorkers)
	chunkSize := (len(candidates) + numWorkers - 1) / numWorkers
	var wg sync.WaitGroup

	for w := range numWorkers {
		start := w * chunkSize
		if start >= len(candidates) {
			break
		}
		end := min(start+chunkSize, len(candidates))

		wg.Add(1)
		go func(chunk []Record) {
			defer wg.Done()
			var localTop []scoredRecord
			for _, rec := range chunk {
				score := CosineSimilarity(queryVector, rec.Vector)
				localTop = append(localTop, scoredRecord{rec: rec, score: score})
			}
			results <- localTop
		}(candidates[start:end])
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	var allScored []scoredRecord
	for chunk := range results {
		allScored = append(allScored, chunk...)
	}

	sort.Slice(allScored, func(i, j int) bool {
		// Descending order of similarity (1 is best)
		if allScored[i].score == allScored[j].score {
			return allScored[i].rec.ID < allScored[j].rec.ID // Deterministic tie-break
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

// Close is a no-op for MemoryStore (satisfies Store interface).
func (s *MemoryStore) Close() error { return nil }

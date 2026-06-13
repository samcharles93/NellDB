package nell

import (
	"fmt"
	"maps"
	"sync"
)

// ── Store Interface ───────────────────────────────────────────────────────────

// Store is the abstract storage layer.  Every backend — in-memory, bbolt,
// IndexedDB — implements this interface.  The sync engine and conflict
// resolver operate on Store, never on a concrete backend.
type Store interface {
	Put(incoming Record) (accepted bool, current Record, err error)
	Get(id string) (Record, error)
	Delete(id string) (Record, error)
	List() ([]Record, error)
	GetChangesSince(clock HLC) ([]Record, error)
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
}

// NewMemoryStore returns an initialised MemoryStore with the given node
// identifier (used as Record.UpdatedBy on every write).
func NewMemoryStore(nodeID string) *MemoryStore {
	return &MemoryStore{
		nodeID:  nodeID,
		clock:   NewHLC(),
		records: make(map[string]Record),
		kv:      make(KnowledgeVector),
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

// Put inserts or updates a record using LWW conflict resolution.
func (s *MemoryStore) Put(incoming Record) (bool, Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Update local HLC with incoming clock
	s.clock.Update(incoming.Clock)

	local, exists := s.records[incoming.ID]
	if !exists {
		s.records[incoming.ID] = incoming
		s.kv.Update(incoming.UpdatedBy, incoming.Clock)
		return true, incoming, nil
	}

	winner := ResolveConflict(&local, &incoming)
	s.records[incoming.ID] = *winner
	s.kv.Update(winner.UpdatedBy, winner.Clock)
	return winner == &incoming, *winner, nil
}

// PutLocal creates or updates a record with a fresh local HLC clock.  This is
// the method the application calls for local writes (as opposed to Put, which
// is used during sync ingestion).
func (s *MemoryStore) PutLocal(rec *Record) (Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rec.Clock = s.clock.Tick()
	rec.UpdatedBy = s.nodeID

	s.records[rec.ID] = *rec
	s.kv.Update(s.nodeID, rec.Clock)
	return *rec, nil
}

// Get returns the record with the given ID, or an error if not found.
func (s *MemoryStore) Get(id string) (Record, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rec, ok := s.records[id]
	if !ok {
		return Record{}, fmt.Errorf("record %q: %w", id, ErrRecordNotFound)
	}
	return rec, nil
}

// Delete tombstones a record by writing a Deleted=true entry with a fresh
// local clock.
func (s *MemoryStore) Delete(id string) (Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rec, ok := s.records[id]
	if !ok {
		rec = Record{ID: id}
	}
	rec.Clock = s.clock.Tick()
	rec.UpdatedBy = s.nodeID
	rec.Deleted = true
	// Keep the existing type so the tombstone travels with the right discriminator.

	s.records[id] = rec
	s.kv.Update(s.nodeID, rec.Clock)
	return rec, nil
}

// List returns all non-deleted records in the logstore.
func (s *MemoryStore) List() ([]Record, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []Record
	for _, r := range s.records {
		if !r.Deleted {
			out = append(out, r)
		}
	}
	return out, nil
}

// GetChangesSince returns every record whose clock is strictly greater than
// the given lower bound.  This is used by the sync protocol to compute deltas.
func (s *MemoryStore) GetChangesSince(since HLC) ([]Record, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []Record
	for _, r := range s.records {
		if r.Clock.GreaterThan(since) {
			out = append(out, r)
		}
	}
	return out, nil
}

// Close is a no-op for MemoryStore (satisfies Store interface).
func (s *MemoryStore) Close() error { return nil }

// ── helpers ───────────────────────────────────────────────────────────────────

package nell

import (
	"fmt"
	"sync"
	"testing"
)

// ── Concurrency ──────────────────────────────────────────────────────────────

func TestMemoryStoreConcurrentPuts(t *testing.T) {
	s := NewMemoryStore("node-a")
	var wg sync.WaitGroup
	const goroutines = 50

	// Concurrent writes to the same record ID — should not panic or corrupt.
	for i := range goroutines {
		wg.Add(1)
		go func(seq int) {
			defer wg.Done()
			_, _ = s.PutLocal(&Record{
				ID:      "shared",
				Type:    TypeText,
				Payload: fmt.Appendf(nil, "goroutine-%d", seq),
			})
		}(i)
	}
	wg.Wait()

	rec, err := s.Get("shared")
	if err != nil {
		t.Fatalf("record lost after concurrent writes: %v", err)
	}
	if rec.Payload == nil {
		t.Error("payload should not be nil after concurrent writes")
	}
}

func TestMemoryStoreConcurrentPutAndList(t *testing.T) {
	s := NewMemoryStore("node-a")
	var wg sync.WaitGroup

	// Pre-populate
	for i := range 10 {
		_, _ = s.PutLocal(&Record{ID: fmt.Sprintf("doc-%d", i), Type: TypeText, Payload: []byte("init")})
	}

	// Simultaneous reads and writes
	for i := range 10 {
		wg.Add(2)
		go func(seq int) {
			defer wg.Done()
			_, _ = s.PutLocal(&Record{ID: fmt.Sprintf("doc-%d", seq+100), Type: TypeText, Payload: []byte("concurrent")})
		}(i)
		go func() {
			defer wg.Done()
			_, _ = s.List()
		}()
	}
	wg.Wait()

	list, _ := s.List()
	if len(list) < 10 {
		t.Errorf("expected at least 10 records, got %d", len(list))
	}
}

func TestMemoryStoreConcurrentDeleteAndGet(t *testing.T) {
	s := NewMemoryStore("node-a")
	_, _ = s.PutLocal(&Record{ID: "target", Type: TypeText, Payload: []byte("live")})

	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			// Delete and immediately try to read — should never panic
			_, _ = s.Delete("target")
			_, _ = s.Get("target")
		})
	}
	wg.Wait()
}

// ── Edge cases ───────────────────────────────────────────────────────────────

func TestMemoryStorePutEmptyID(t *testing.T) {
	s := NewMemoryStore("node-a")
	if _, err := s.PutLocal(&Record{ID: "", Type: TypeText, Payload: []byte("no-id")}); err != nil {
		t.Fatalf("PutLocal with empty ID failed: %v", err)
	}
	got, err := s.Get("")
	if err != nil {
		t.Fatalf("Get with empty ID failed: %v", err)
	}
	if string(got.Payload) != "no-id" {
		t.Error("payload mismatch for empty-ID record")
	}
}

func TestMemoryStoreGetNonExistent(t *testing.T) {
	s := NewMemoryStore("node-a")
	_, err := s.Get("does-not-exist")
	if err == nil {
		t.Error("expected error for non-existent record, got nil")
	}
}

func TestMemoryStoreDeleteNonExistent(t *testing.T) {
	s := NewMemoryStore("node-a")
	rec, err := s.Delete("never-created")
	if err != nil {
		t.Fatalf("delete of non-existent record should create a tombstone: %v", err)
	}
	if !rec.Deleted {
		t.Error("tombstone should be marked deleted")
	}
	if rec.ID != "never-created" {
		t.Errorf("tombstone ID = %q, want never-created", rec.ID)
	}
}

func TestMemoryStoreListEmpty(t *testing.T) {
	s := NewMemoryStore("node-a")
	list, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Errorf("empty store should return empty list, got %d records", len(list))
	}
}

func TestMemoryStoreGetChangesSinceFutureClock(t *testing.T) {
	s := NewMemoryStore("node-a")
	_, _ = s.PutLocal(&Record{ID: "doc-1", Type: TypeText, Payload: []byte("a")})

	// Query with a clock far in the future
	farAhead := HLC{WallTime: 99999999999999, Counter: 999}
	changes, err := s.GetChangesSince(farAhead)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 0 {
		t.Errorf("expected 0 changes for future clock, got %d", len(changes))
	}
}

func TestMemoryStoreGetChangesSinceZeroClock(t *testing.T) {
	s := NewMemoryStore("node-a")
	_, _ = s.PutLocal(&Record{ID: "doc-1", Type: TypeText, Payload: []byte("a")})

	changes, err := s.GetChangesSince(HLC{})
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 {
		t.Errorf("expected 1 change since zero clock, got %d", len(changes))
	}
}

func TestMemoryStorePutWithFutureClock(t *testing.T) {
	s := NewMemoryStore("node-a")
	_, _ = s.PutLocal(&Record{ID: "doc-1", Type: TypeText, Payload: []byte("local")})

	// Remote write from far in the future — should be accepted
	future := Record{
		ID:        "doc-1",
		Type:      TypeText,
		Payload:   []byte("from-the-future"),
		Clock:     HLC{WallTime: 99999999999999, Counter: 0},
		UpdatedBy: "remote-node",
	}
	accepted, current, err := s.Put(future)
	if err != nil {
		t.Fatal(err)
	}
	if !accepted {
		t.Error("future clock should always win")
	}
	if string(current.Payload) != "from-the-future" {
		t.Errorf("payload = %q, want from-the-future", current.Payload)
	}
}

// ── HLC stress ───────────────────────────────────────────────────────────────

func TestHLCMonotonic(t *testing.T) {
	h := NewHLC()
	prev := h
	for range 1000 {
		next := h.Tick()
		if !next.GreaterThan(prev) {
			t.Errorf("HLC not monotonic at iteration: prev=%v next=%v", prev, next)
		}
		prev = next
	}
}

func TestHLCUpdateBackwardsClock(t *testing.T) {
	h := NewHLC()
	h.WallTime = 99999999999999

	// Peer sends an older clock — should not regress
	older := HLC{WallTime: 1000, Counter: 0}
	h.Update(older)
	if h.WallTime != 99999999999999 {
		t.Error("Update with older clock regressed WallTime")
	}
}

func TestHLCEqualGreaterThan(t *testing.T) {
	a := HLC{WallTime: 1000, Counter: 5}
	b := HLC{WallTime: 1000, Counter: 5}
	if a.GreaterThan(b) || b.GreaterThan(a) {
		t.Error("equal clocks should not be greater than each other")
	}
	if !a.Equal(b) {
		t.Error("equal clocks should be Equal")
	}
}

func TestHLCUpdateSameWallHigherCounter(t *testing.T) {
	h := HLC{WallTime: 1000, Counter: 1}
	peer := HLC{WallTime: 1000, Counter: 5}
	h.Update(peer)
	// Peer has same wall time but higher counter → local should advance past it
	if h.WallTime != 1000 {
		t.Errorf("WallTime should remain 1000, got %d", h.WallTime)
	}
	if h.Counter != 6 {
		t.Errorf("Counter should be peer.Counter+1 = 6, got %d", h.Counter)
	}
}

func TestHLCUpdateSameWallLowerCounter(t *testing.T) {
	h := HLC{WallTime: 1000, Counter: 5}
	peer := HLC{WallTime: 1000, Counter: 1}
	h.Update(peer)
	// Peer has same wall time but lower counter → local should NOT regress
	if h.WallTime != 1000 {
		t.Errorf("WallTime should remain 1000, got %d", h.WallTime)
	}
	if h.Counter != 5 {
		t.Errorf("Counter should remain 5 (no regression), got %d", h.Counter)
	}
}

func TestHLCUpdateLaterWallTime(t *testing.T) {
	h := HLC{WallTime: 1000, Counter: 5}
	peer := HLC{WallTime: 2000, Counter: 3}
	h.Update(peer)
	// Peer has later wall time → local should jump ahead
	if h.WallTime != 2000 {
		t.Errorf("WallTime should be 2000, got %d", h.WallTime)
	}
	if h.Counter != 4 {
		t.Errorf("Counter should be peer.Counter+1 = 4, got %d", h.Counter)
	}
}

func TestHLCString(t *testing.T) {
	h := HLC{WallTime: 1234567890, Counter: 42}
	if h.String() != "1234567890:42" {
		t.Errorf("String() = %q, want 1234567890:42", h.String())
	}
}

// ── KnowledgeVector ──────────────────────────────────────────────────────────

func TestKnowledgeVectorUpdateMonotonic(t *testing.T) {
	kv := make(KnowledgeVector)
	kv.Update("node-a", HLC{WallTime: 1000, Counter: 0})
	kv.Update("node-a", HLC{WallTime: 500, Counter: 999}) // older WallTime
	if kv["node-a"].WallTime != 1000 {
		t.Error("KnowledgeVector should not regress to older clock")
	}
}

func TestKnowledgeVectorMultipleNodes(t *testing.T) {
	kv := make(KnowledgeVector)
	kv.Update("node-a", HLC{WallTime: 1000, Counter: 0})
	kv.Update("node-b", HLC{WallTime: 2000, Counter: 0})

	if len(kv) != 2 {
		t.Errorf("expected 2 entries, got %d", len(kv))
	}
	if kv["node-a"].WallTime != 1000 {
		t.Error("node-a entry incorrect")
	}
	if kv["node-b"].WallTime != 2000 {
		t.Error("node-b entry incorrect")
	}
}

// ── LWW edge cases ───────────────────────────────────────────────────────────

func TestLWWIdenticalClocks(t *testing.T) {
	clock := HLC{WallTime: 1000, Counter: 0}
	local := Record{ID: "doc", Type: TypeText, Payload: []byte("local"), Clock: clock, UpdatedBy: "aaa"}
	remote := Record{ID: "doc", Type: TypeText, Payload: []byte("remote"), Clock: clock, UpdatedBy: "zzz"}

	winner := ResolveConflict(&local, &remote)
	// "zzz" > "aaa" lexically, so remote should win
	if winner.UpdatedBy != "zzz" {
		t.Errorf("tie-break should pick lexically greater node: got %s", winner.UpdatedBy)
	}
}

func TestLWWTombstoneWinsOverLiveWithLowerClock(t *testing.T) {
	live := Record{ID: "doc", Type: TypeText, Payload: []byte("alive"), Clock: HLC{WallTime: 1000, Counter: 0}, UpdatedBy: "a", Deleted: false}
	tombstone := Record{ID: "doc", Type: TypeText, Clock: HLC{WallTime: 2000, Counter: 0}, UpdatedBy: "b", Deleted: true}

	winner := ResolveConflict(&live, &tombstone)
	if !winner.Deleted {
		t.Error("tombstone with higher clock should win over live record")
	}
}

func TestLWWLiveWinsOverTombstoneWithLowerClock(t *testing.T) {
	tombstone := Record{ID: "doc", Type: TypeText, Clock: HLC{WallTime: 1000, Counter: 0}, UpdatedBy: "a", Deleted: true}
	live := Record{ID: "doc", Type: TypeText, Payload: []byte("resurrected"), Clock: HLC{WallTime: 2000, Counter: 0}, UpdatedBy: "b", Deleted: false}

	winner := ResolveConflict(&tombstone, &live)
	if winner.Deleted {
		t.Error("live record with higher clock should win over tombstone")
	}
}

func TestLWWTieBreakLocal(t *testing.T) {
	clock := HLC{WallTime: 1000, Counter: 0}
	// Local node comes first alphabetically → local should win
	local := Record{ID: "doc", Type: TypeText, Payload: []byte("local-wins"), Clock: clock, UpdatedBy: "zzz"}
	remote := Record{ID: "doc", Type: TypeText, Payload: []byte("remote-loses"), Clock: clock, UpdatedBy: "aaa"}

	winner := ResolveConflict(&local, &remote)
	if winner.UpdatedBy != "zzz" {
		t.Errorf("tie-break: got %s, want zzz", winner.UpdatedBy)
	}
	if string(winner.Payload) != "local-wins" {
		t.Error("lexically greater nodeID should win on tie")
	}
}

func TestLWWDeterministicAcrossOrder(t *testing.T) {
	clock := HLC{WallTime: 1000, Counter: 0}
	a := Record{ID: "doc", Type: TypeText, Payload: []byte("a"), Clock: clock, UpdatedBy: "x"}
	b := Record{ID: "doc", Type: TypeText, Payload: []byte("b"), Clock: clock, UpdatedBy: "y"}

	// Order shouldn't matter
	w1 := ResolveConflict(&a, &b)
	w2 := ResolveConflict(&b, &a)

	if w1.UpdatedBy != w2.UpdatedBy {
		t.Error("LWW should be deterministic regardless of argument order")
	}
}

// ── KnowledgeVector edge cases ──────────────────────────────────────────────

func TestKnowledgeVectorEmpty(t *testing.T) {
	kv := make(KnowledgeVector)
	if len(kv) != 0 {
		t.Error("new KnowledgeVector should be empty")
	}
	// Update on empty should work
	kv.Update("node-a", HLC{WallTime: 1000, Counter: 0})
	if kv["node-a"].WallTime != 1000 {
		t.Error("first update should set clock")
	}
}

func TestKnowledgeVectorUpdateNewerCounterSameWall(t *testing.T) {
	kv := make(KnowledgeVector)
	kv.Update("node-a", HLC{WallTime: 1000, Counter: 0})
	kv.Update("node-a", HLC{WallTime: 1000, Counter: 5})
	if kv["node-a"].Counter != 5 {
		t.Errorf("should update to higher counter: got %d, want 5", kv["node-a"].Counter)
	}
}

func TestKnowledgeVectorCopyIndependence(t *testing.T) {
	s := NewMemoryStore("node-a")
	_, _ = s.PutLocal(&Record{ID: "doc", Type: TypeText, Payload: []byte("data")})

	kv1 := s.KnowledgeVector()
	kv2 := s.KnowledgeVector()

	// Mutate kv1 — should not affect kv2 or the store
	kv1["intruder"] = HLC{WallTime: 999, Counter: 0}

	kv3 := s.KnowledgeVector()
	if _, ok := kv3["intruder"]; ok {
		t.Error("KnowledgeVector copy should not be affected by external mutations")
	}
	_ = kv2
}

// ── Large / stress ───────────────────────────────────────────────────────────

func TestMemoryStoreManyRecords(t *testing.T) {
	s := NewMemoryStore("node-a")
	const n = 10000
	for i := range n {
		_, _ = s.PutLocal(&Record{ID: fmt.Sprintf("doc-%05d", i), Type: TypeText, Payload: []byte("data")})
	}
	list, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != n {
		t.Errorf("expected %d records, got %d", n, len(list))
	}
}

func TestMemoryStoreLargePayload(t *testing.T) {
	s := NewMemoryStore("node-a")
	large := make([]byte, 1<<20) // 1 MB
	for i := range large {
		large[i] = byte(i % 256)
	}
	_, err := s.PutLocal(&Record{ID: "big-one", Type: TypeImage, Payload: large})
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Get("big-one")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Payload) != len(large) {
		t.Errorf("payload length: got %d, want %d", len(got.Payload), len(large))
	}
	// Verify first and last bytes
	if got.Payload[0] != 0 || got.Payload[len(got.Payload)-1] != byte((len(large)-1)%256) {
		t.Error("payload corrupted")
	}
}

func TestMemoryStoreGetChangesSinceEmpty(t *testing.T) {
	s := NewMemoryStore("node-a")
	changes, err := s.GetChangesSince(NewHLC())
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 0 {
		t.Errorf("empty store should return no changes, got %d", len(changes))
	}
}

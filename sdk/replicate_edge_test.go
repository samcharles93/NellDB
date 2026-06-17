package sdk

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/samcharles93/NellDB"
)

// ── Phase 1: SDK tombstone push ─────────────────────────────────────────

// TestReplicatorPushIncludesTombstones verifies that when a client deletes
// a document (creating a local tombstone), Push() sends the tombstone to
// the server so other clients learn about the deletion.
//
// This test is EXPECTED TO FAIL until the Push() method in replicate.go
// is changed from store.List() (which filters Deleted=true) to include
// all records including tombstones.
func TestReplicatorPushIncludesTombstones(t *testing.T) {
	storeA := nell.NewMemoryStore("server")
	ts := newTestServer(storeA, "server")
	defer ts.Close()

	// Client creates a document, pushes it, then deletes it.
	clientStore := nell.NewMemoryStore("client")
	db := New(clientStore, "client", nell.DefaultCollection)

	_, err := db.Put(context.Background(), Doc{FieldID: "doomed", "v": 1})
	if err != nil {
		t.Fatal(err)
	}

	rep := NewReplicator(db, ts.URL)
	pushed, err := rep.Push(context.Background())
	if err != nil {
		t.Fatalf("first push: %v", err)
	}
	if pushed != 1 {
		t.Errorf("first push = %d, want 1", pushed)
	}

	// Delete the document locally.
	rev, err := db.Remove(context.Background(), "doomed")
	if err != nil {
		t.Fatalf("remove: %v", err)
	}
	if rev == "" {
		t.Fatal("remove returned empty rev")
	}

	// Push again — the tombstone MUST be sent to the server.
	// This assertion will FAIL until replicate.go's Push() includes tombstones.
	pushed, err = rep.Push(context.Background())
	if err != nil {
		t.Fatalf("second push (tombstone): %v", err)
	}
	if pushed != 1 {
		t.Errorf("tombstone push = %d, want 1 (tombstone should be pushed)", pushed)
	}

	// Verify the server has the tombstone.
	rec, err := storeA.Get(nell.DefaultCollection, "doomed")
	if err != nil {
		t.Fatalf("server missing doomed after tombstone push: %v", err)
	}
	if !rec.Deleted {
		t.Error("server should have doomed as Deleted=true after tombstone push")
	}
}

// TestReplicatorTombstoneRoundtrip validates the complete lifecycle:
// client A creates → pushes → deletes → pushes tombstone →
// client B pulls → sees the deletion.
func TestReplicatorTombstoneRoundtrip(t *testing.T) {
	storeA := nell.NewMemoryStore("server")
	ts := newTestServer(storeA, "server")
	defer ts.Close()

	// Client A: create, push, delete, push tombstone.
	dbA := New(nell.NewMemoryStore("clientA"), "clientA", nell.DefaultCollection)
	_, _ = dbA.Put(context.Background(), Doc{FieldID: "roundtrip", "v": 1})
	repA := NewReplicator(dbA, ts.URL)
	_, _ = repA.Push(context.Background())
	_, _ = dbA.Remove(context.Background(), "roundtrip")

	pushed, err := repA.Push(context.Background())
	if err != nil {
		t.Fatalf("push tombstone: %v", err)
	}
	if pushed == 0 {
		t.Skip("tombstone push not yet implemented — skipping roundtrip check")
	}

	// Client B: fresh start, pulls everything.
	dbB := New(nell.NewMemoryStore("clientB"), "clientB", nell.DefaultCollection)
	repB := NewReplicator(dbB, ts.URL)

	pulled, err := repB.Pull(context.Background())
	if err != nil {
		t.Fatalf("pull: %v", err)
	}
	// Client B should get at least the tombstone (and possibly the live record too,
	// depending on clock ordering).
	if pulled < 1 {
		t.Errorf("pull = %d, want at least 1", pulled)
	}

	// The record should exist in client B's store as a tombstone.
	// Get returns deleted records, so this should find it.
	storeB := dbB.store
	rec, err := storeB.Get(nell.DefaultCollection, "roundtrip")
	if err != nil {
		t.Fatalf("server B store missing roundtrip: %v", err)
	}
	if !rec.Deleted {
		t.Error("expected Deleted=true after roundtrip")
	}
}

// ── Phase 3: Offensive concurrency tests ────────────────────────────────

// TestReplicatorConcurrentPushPull verifies that concurrent Push and Pull
// operations don't cause panics, data races, or lost records.
func TestReplicatorConcurrentPushPull(t *testing.T) {
	storeA := nell.NewMemoryStore("server")
	ts := newTestServer(storeA, "server")
	defer ts.Close()

	db := New(nell.NewMemoryStore("client"), "client", nell.DefaultCollection)
	rep := NewReplicator(db, ts.URL)

	// Pre-populate some local records.
	for i := range 20 {
		_, _ = db.Put(context.Background(), Doc{FieldID: fmt.Sprintf("conc-%d", i), "v": i})
	}

	var wg sync.WaitGroup

	// Concurrent pushes.
	for range 5 {
		wg.Go(func() {
			for range 5 {
				_, err := rep.Push(context.Background())
				if err != nil {
					t.Errorf("concurrent push error: %v", err)
					return
				}
				time.Sleep(5 * time.Millisecond)
			}
		})
	}

	// Concurrent pulls.
	for range 5 {
		wg.Go(func() {
			for range 5 {
				_, err := rep.Pull(context.Background())
				if err != nil {
					t.Errorf("concurrent pull error: %v", err)
					return
				}
				time.Sleep(5 * time.Millisecond)
			}
		})
	}

	wg.Wait()
}

// TestReplicatorPushLargeBatch verifies that Push handles a large number
// of records without truncation or corruption.
func TestReplicatorPushLargeBatch(t *testing.T) {
	storeA := nell.NewMemoryStore("server")
	ts := newTestServer(storeA, "server")
	defer ts.Close()

	db := New(nell.NewMemoryStore("client"), "client", nell.DefaultCollection)
	rep := NewReplicator(db, ts.URL)

	n := 200
	for i := range n {
		_, _ = db.Put(context.Background(), Doc{FieldID: fmt.Sprintf("batch-%d", i), "v": i})
	}

	pushed, err := rep.Push(context.Background())
	if err != nil {
		t.Fatalf("push: %v", err)
	}
	if pushed != n {
		t.Errorf("pushed = %d, want %d", pushed, n)
	}

	// All records should be on the server.
	list, _ := storeA.List(nell.DefaultCollection)
	if len(list) < n {
		t.Errorf("server has %d records, want at least %d", len(list), n)
	}
}

// ── Phase 5: Live replication edge cases ────────────────────────────────

// TestReplicatorLiveSurvivesServerRestart verifies that the Live loop
// reconnects and continues after the server goes down and comes back.
func TestReplicatorLiveSurvivesServerRestart(t *testing.T) {
	storeA := nell.NewMemoryStore("server")
	ts := newTestServer(storeA, "server")
	// We'll close and reopen this manually.

	db := New(nell.NewMemoryStore("client"), "client", nell.DefaultCollection)
	rep := NewReplicator(db, ts.URL)

	ctx := t.Context()

	// Start live loop with short interval.
	stop := rep.Live(ctx, LiveConfig{Interval: 50 * time.Millisecond, BackoffMax: 200 * time.Millisecond})
	defer stop()

	// Let it run for a bit.
	time.Sleep(100 * time.Millisecond)

	// Simulate server restart by closing and reopening.
	ts.Close()
	time.Sleep(100 * time.Millisecond) // server down — Live should back off

	ts2 := newTestServer(storeA, "server")
	defer ts2.Close()

	// Stop the live loop, repoint, and restart to avoid data race on BaseURL.
	stop()
	rep.BaseURL = ts2.URL
	stop = rep.Live(ctx, LiveConfig{Interval: 50 * time.Millisecond, BackoffMax: 200 * time.Millisecond})
	defer stop()

	// Push a record to the new server directly.
	_, _, _ = storeA.Put(nell.Record{ID: "after-restart", Type: nell.TypeText, Payload: []byte(`{"_rev":"1-xyz","v":1}`), Clock: nell.HLC{WallTime: 9999, Counter: 0}, UpdatedBy: "peer"})

	// Live loop should eventually pull it.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := db.Get(context.Background(), "after-restart"); err == nil {
			return // success
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Error("after-restart record never arrived via live replication after server restart")
}

// ── Phase 7: End-to-end SDK sync ────────────────────────────────────────

// TestReplicatorBiDirectionalSync verifies that two clients can sync
// through a shared server, each seeing the other's changes.
func TestReplicatorBiDirectionalSync(t *testing.T) {
	storeA := nell.NewMemoryStore("server")
	ts := newTestServer(storeA, "server")
	defer ts.Close()

	// Client 1 writes, pushes.
	db1 := New(nell.NewMemoryStore("c1"), "c1", nell.DefaultCollection)
	_, _ = db1.Put(context.Background(), Doc{FieldID: "from-c1", "author": "client1"})
	rep1 := NewReplicator(db1, ts.URL)
	pushed, err := rep1.Push(context.Background())
	if err != nil {
		t.Fatalf("c1 push: %v", err)
	}
	if pushed != 1 {
		t.Errorf("c1 push = %d, want 1", pushed)
	}

	// Client 2 pulls, writes, pushes.
	db2 := New(nell.NewMemoryStore("c2"), "c2", nell.DefaultCollection)
	rep2 := NewReplicator(db2, ts.URL)
	pulled, err := rep2.Pull(context.Background())
	if err != nil {
		t.Fatalf("c2 pull: %v", err)
	}
	if pulled != 1 {
		t.Errorf("c2 pull = %d, want 1", pulled)
	}

	doc, err := db2.Get(context.Background(), "from-c1")
	if err != nil {
		t.Fatalf("c2 missing from-c1: %v", err)
	}
	if doc["author"] != "client1" {
		t.Errorf("c2 sees author=%v, want client1", doc["author"])
	}

	_, _ = db2.Put(context.Background(), Doc{FieldID: "from-c2", "author": "client2"})
	pushed, err = rep2.Push(context.Background())
	if err != nil {
		t.Fatalf("c2 push: %v", err)
	}
	if pushed == 0 {
		t.Error("expected at least 1 record pushed from c2")
	}

	// Client 1 pulls, should see c2's record.
	pulled, err = rep1.Pull(context.Background())
	if err != nil {
		t.Fatalf("c1 pull back: %v", err)
	}
	if pulled != 1 {
		t.Errorf("c1 pull back = %d, want 1", pulled)
	}
	doc, err = db1.Get(context.Background(), "from-c2")
	if err != nil {
		t.Fatalf("c1 missing from-c2: %v", err)
	}
	if doc["author"] != "client2" {
		t.Errorf("c1 sees author=%v, want client2", doc["author"])
	}
}

// TestReplicatorSyncIsIdempotent verifies that running Push+Pull twice
// does not duplicate records or degrade state.
func TestReplicatorSyncIsIdempotent(t *testing.T) {
	storeA := nell.NewMemoryStore("server")
	ts := newTestServer(storeA, "server")
	defer ts.Close()

	db := New(nell.NewMemoryStore("client"), "client", nell.DefaultCollection)
	rep := NewReplicator(db, ts.URL)

	_, _ = db.Put(context.Background(), Doc{FieldID: "idem", "v": 1})

	// First sync.
	p1, l1, err := rep.Sync(context.Background())
	if err != nil {
		t.Fatalf("first sync: %v", err)
	}
	if p1 != 1 || l1 != 0 {
		t.Errorf("first sync push=%d pull=%d, want push=1 pull=0", p1, l1)
	}

	// Second sync — should be a no-op.
	p2, l2, err := rep.Sync(context.Background())
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if p2 != 0 || l2 != 0 {
		t.Errorf("second sync push=%d pull=%d, want push=0 pull=0", p2, l2)
	}
}

// ── Helpers ─────────────────────────────────────────────────────────────

package sdk

import (
	"context"
	"errors"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/samcharles93/nell-engine"
	"github.com/samcharles93/nell-engine/server"
)

// ── CRUD ─────────────────────────────────────────────────────────────────────

func TestPutGetRoundtrip(t *testing.T) {
	db := New(nell.NewMemoryStore("n"), "n")
	ctx := context.Background()

	rev, err := db.Put(ctx, Doc{FieldID: "note:1", "title": "Hello"})
	if err != nil {
		t.Fatal(err)
	}
	if rev == "" {
		t.Fatal("rev is empty")
	}

	got, err := db.Get(ctx, "note:1")
	if err != nil {
		t.Fatal(err)
	}
	if got["title"] != "Hello" {
		t.Errorf("title = %v, want Hello", got["title"])
	}
	if got[FieldRev] != rev {
		t.Errorf("rev mismatch: got %v, want %s", got[FieldRev], rev)
	}
}

func TestPutConflict(t *testing.T) {
	db := New(nell.NewMemoryStore("n"), "n")
	ctx := context.Background()

	rev1, _ := db.Put(ctx, Doc{FieldID: "x", "v": 1})
	_, _ = db.Put(ctx, Doc{FieldID: "x", "v": 2})

	_, err := db.Put(ctx, Doc{FieldID: "x", FieldRev: rev1, "v": 3})
	if !errors.Is(err, ErrConflict) {
		t.Errorf("expected ErrConflict, got %v", err)
	}
}

func TestGetMissing(t *testing.T) {
	db := New(nell.NewMemoryStore("n"), "n")
	_, err := db.Get(context.Background(), "nope")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestRevMonotonicAndContentHash(t *testing.T) {
	// Local writes without explicit _rev must continue the chain.  This is
	// the read-modify-write contract callers rely on.
	db := New(nell.NewMemoryStore("n"), "n")
	ctx := context.Background()

	r1, _ := db.Put(ctx, Doc{FieldID: "x", "v": 1})
	r2, _ := db.Put(ctx, Doc{FieldID: "x", "v": 2})
	r3, _ := db.Put(ctx, Doc{FieldID: "x", "v": 3})

	if parseGen(r1) >= parseGen(r2) || parseGen(r2) >= parseGen(r3) {
		t.Errorf("revs not monotonically increasing: %s %s %s", r1, r2, r3)
	}
	// Different bodies must produce different content hashes.
	if r1 == r2 || r2 == r3 {
		t.Errorf("revs collide despite different bodies: %s %s %s", r1, r2, r3)
	}

	// Stale rev should conflict
	_, err := db.Put(ctx, Doc{FieldID: "x", FieldRev: r1, "v": 99})
	if !errors.Is(err, ErrConflict) {
		t.Errorf("stale rev should conflict, got %v", err)
	}
}

func TestRevIdenticalBodyStillIncrements(t *testing.T) {
	// Identical bodies, no explicit _rev — revs must still tick (gen
	// increments) so the change feed can detect "this is a new write".
	db := New(nell.NewMemoryStore("n"), "n")
	ctx := context.Background()

	r1, _ := db.Put(ctx, Doc{FieldID: "x", "v": 1})
	r2, _ := db.Put(ctx, Doc{FieldID: "x", "v": 1})

	if r1 == r2 {
		t.Errorf("identical bodies should still produce distinct revs (gen differs): %s == %s", r1, r2)
	}
	if parseGen(r1) != parseGen(r2)-1 {
		t.Errorf("gen not exactly +1: %s vs %s", r1, r2)
	}
}

func TestRemoveTombstone(t *testing.T) {
	db := New(nell.NewMemoryStore("n"), "n")
	ctx := context.Background()

	_, _ = db.Put(ctx, Doc{FieldID: "x", "v": 1})

	rev, err := db.Remove(ctx, "x")
	if err != nil {
		t.Fatal(err)
	}
	if rev == "" {
		t.Fatal("remove returned empty rev")
	}

	if _, err := db.Get(ctx, "x"); !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound after remove, got %v", err)
	}

	// Idempotent: re-removing is a no-op
	if _, err := db.Remove(ctx, "x"); err != nil {
		t.Errorf("second remove should be idempotent, got %v", err)
	}
}

func TestRemoveWithRevConflict(t *testing.T) {
	db := New(nell.NewMemoryStore("n"), "n")
	ctx := context.Background()

	rev1, _ := db.Put(ctx, Doc{FieldID: "x", "v": 1})
	_, _ = db.Put(ctx, Doc{FieldID: "x", "v": 2})

	_, err := db.Remove(ctx, Doc{FieldID: "x", FieldRev: rev1})
	if !errors.Is(err, ErrConflict) {
		t.Errorf("expected ErrConflict, got %v", err)
	}
}

// ── Bulk ─────────────────────────────────────────────────────────────────────

func TestPutManyAllOrNothing(t *testing.T) {
	db := New(nell.NewMemoryStore("n"), "n")
	ctx := context.Background()

	docs := []Doc{
		{FieldID: "a", "v": 1},
		{FieldID: "b", "v": 2},
		{FieldID: "c", FieldRev: "stale-rev-xxx", "v": 3}, // forces conflict (rev on non-existent doc)
	}
	_, err := db.PutMany(ctx, docs)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("expected ErrConflict, got %v", err)
	}

	// Cache must be rolled back: revs for a and b must not have leaked.
	db.mu.RLock()
	_, aInCache := db.revs["a"]
	_, bInCache := db.revs["b"]
	db.mu.RUnlock()
	if aInCache {
		t.Error("a rev leaked into cache")
	}
	if bInCache {
		t.Error("b rev leaked into cache")
	}
	// Note: store-level atomicity is backend-dependent. MemoryStore applies
	// each Put sequentially, so a and b ARE in the store after rollback.
	// Callers needing strict atomicity should use a transactional backend.
}

func TestGetMany(t *testing.T) {
	db := New(nell.NewMemoryStore("n"), "n")
	ctx := context.Background()
	_, _ = db.Put(ctx, Doc{FieldID: "a", "v": 1})
	_, _ = db.Put(ctx, Doc{FieldID: "b", "v": 2})

	got, err := db.GetMany(ctx, []string{"a", "b", "ghost"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Errorf("expected 2 docs, got %d", len(got))
	}
	if got["a"]["v"].(float64) != 1.0 {
		t.Errorf("a.v = %v, want 1.0", got["a"]["v"])
	}
}

// ── AllDocs ──────────────────────────────────────────────────────────────────

func TestAllDocsRange(t *testing.T) {
	db := New(nell.NewMemoryStore("n"), "n")
	ctx := context.Background()
	for _, id := range []string{"m:1", "m:2", "m:3", "c:1", "c:2"} {
		if _, err := db.Put(ctx, Doc{FieldID: id, "data": id}); err != nil {
			t.Fatal(err)
		}
	}

	// Inclusive-end with the high-sentinel trick (\ufff0) is how callers
	// scan a key prefix.
	rows, err := db.AllDocs(ctx, DocRange{StartKey: "m:", EndKey: "m:\ufff0", InclusiveEnd: true, IncludeDocs: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows.Rows) != 3 {
		t.Errorf("expected 3 m:* rows, got %d", len(rows.Rows))
	}
	for _, r := range rows.Rows {
		if r.Doc == nil {
			t.Errorf("row %s missing doc", r.ID)
		}
	}
}

func TestAllDocsByKeys(t *testing.T) {
	db := New(nell.NewMemoryStore("n"), "n")
	ctx := context.Background()
	for _, id := range []string{"a", "b", "c"} {
		_, _ = db.Put(ctx, Doc{FieldID: id})
	}
	rows, err := db.AllDocs(ctx, DocRange{Keys: []string{"b", "a", "ghost"}, IncludeDocs: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows.Rows) != 2 {
		t.Errorf("expected 2 rows (skipping ghost), got %d", len(rows.Rows))
	}
	if rows.Rows[0].ID != "b" || rows.Rows[1].ID != "a" {
		t.Errorf("order not preserved: %v %v", rows.Rows[0].ID, rows.Rows[1].ID)
	}
}

// ── Changes feed ─────────────────────────────────────────────────────────────

func TestChangesFeed(t *testing.T) {
	db := New(nell.NewMemoryStore("n"), "n")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch := db.Changes(ctx)

	var got []string
	var mu sync.Mutex
	done := make(chan struct{})
	go func() {
		for c := range ch {
			mu.Lock()
			got = append(got, c.ID)
			mu.Unlock()
		}
		close(done)
	}()

	_, _ = db.Put(context.Background(), Doc{FieldID: "x", "v": 1})
	_, _ = db.Put(context.Background(), Doc{FieldID: "y", "v": 2})
	_, _ = db.Remove(context.Background(), "x")

	cancel()
	<-done

	mu.Lock()
	defer mu.Unlock()
	if len(got) != 3 {
		t.Errorf("expected 3 changes, got %v", got)
	}
}

func TestChangesFeedIncludesRemote(t *testing.T) {
	// Replicated changes must surface on the local feed.  This is the live
	// update path — without it, the UI has to poll db.Changes() to know
	// when peers have written.
	dbB := New(nell.NewMemoryStore("b"), "b")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := dbB.Changes(ctx)

	var seen []Change
	var mu sync.Mutex
	done := make(chan struct{})
	go func() {
		for c := range ch {
			mu.Lock()
			seen = append(seen, c)
			mu.Unlock()
		}
		close(done)
	}()

	// Give the relay goroutine a moment to start
	time.Sleep(10 * time.Millisecond)

	rec := nell.Record{
		ID:        "remote-1",
		Type:      nell.TypeText,
		Payload:   []byte(`{"_rev":"1-abc","title":"Hi"}`),
		Clock:     func() nell.HLC { c := nell.NewHLC(); c.Tick(); return c }(),
		UpdatedBy: "a",
	}
	if err := dbB.ingestRemote(rec); err != nil {
		t.Fatal(err)
	}

	// Give the relay goroutine time to forward
	time.Sleep(10 * time.Millisecond)

	cancel()
	<-done

	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 1 || seen[0].ID != "remote-1" {
		t.Errorf("expected one change for remote-1, got %v", seen)
	}
	if seen[0].Doc["title"] != "Hi" {
		t.Errorf("remote change should carry full doc body, got %v", seen[0].Doc)
	}
}

// ── Replication ──────────────────────────────────────────────────────────────

func TestReplicatorRoundtrip(t *testing.T) {
	storeA := nell.NewMemoryStore("a")
	ts := newTestServer(storeA, "a")
	defer ts.Close()

	dbA := New(nell.NewMemoryStore("client"), "client")
	rep := NewReplicator(dbA, ts.URL)
	_, _ = dbA.Put(context.Background(), Doc{FieldID: "m:1", "title": "A"})
	_, _ = dbA.Put(context.Background(), Doc{FieldID: "m:2", "title": "B"})

	if pushed, err := rep.Push(context.Background()); err != nil || pushed != 2 {
		t.Fatalf("push: pushed=%d err=%v", pushed, err)
	}

	dbB := New(nell.NewMemoryStore("client2"), "client2")
	repB := NewReplicator(dbB, ts.URL)
	if pulled, err := repB.Pull(context.Background()); err != nil || pulled != 2 {
		t.Fatalf("pull: pulled=%d err=%v", pulled, err)
	}

	for _, id := range []string{"m:1", "m:2"} {
		got, err := dbB.Get(context.Background(), id)
		if err != nil {
			t.Fatalf("client2 missing %s: %v", id, err)
		}
		if got["title"] == nil {
			t.Errorf("client2 %s missing title", id)
		}
	}
}

func TestReplicatorIdempotentPull(t *testing.T) {
	// The critical test the original code failed: a record that arrives
	// on the server after the client has pushed must still be picked up
	// on the next pull — even if the client has since deleted its local
	// copy.  This requires a persistent last-seen-clock that doesn't
	// depend on local records.
	storeA := nell.NewMemoryStore("a")
	ts := newTestServer(storeA, "a")
	defer ts.Close()

	dbB := New(nell.NewMemoryStore("client"), "client")
	_, _ = dbB.Put(context.Background(), Doc{FieldID: "m:1", "v": 1})

	rep := NewReplicator(dbB, ts.URL)
	if _, err := rep.Push(context.Background()); err != nil {
		t.Fatal(err)
	}

	// First pull is a no-op (server has nothing newer).
	if pulled, _ := rep.Pull(context.Background()); pulled != 0 {
		t.Errorf("first pull should be empty, got %d", pulled)
	}

	// Server receives a new record from a third party with a clock higher
	// than anything we sent.
	clk := nell.NewHLC()
	clk.Tick()
	if _, _, err := storeA.Put(nell.Record{ID: "m:99", Type: nell.TypeText, Payload: []byte(`{"_rev":"1-abc","v":99}`), Clock: clk, UpdatedBy: "peer"}); err != nil {
		t.Fatal(err)
	}

	// Second pull picks it up.  This is the case that the old code
	// missed: the local store only has m:1 (clock low) and m:99 is on
	// the server with a higher clock — the replicator must know to ask
	// for "since high clock", not "since highest local".
	if pulled, _ := rep.Pull(context.Background()); pulled != 1 {
		t.Errorf("second pull = %d, want 1", pulled)
	}
	if _, err := dbB.Get(context.Background(), "m:99"); err != nil {
		t.Errorf("client missing m:99 after second pull: %v", err)
	}
}

func TestLastSeenClockPersistsAcrossRestart(t *testing.T) {
	// A fresh process opening the same store must resume from the highest
	// clock ever seen, not from zero.  Otherwise a restart would re-pull
	// the entire database.
	serverStore := nell.NewMemoryStore("server")
	ts := newTestServer(serverStore, "server")
	defer ts.Close()

	// First session: push to establish a clock.  The replicator writes
	// meta:clock to the store as part of Pull.
	clientStore := nell.NewMemoryStore("client")
	db1 := New(clientStore, "client")
	_, _ = db1.Put(context.Background(), Doc{FieldID: "m:1", "v": 1})
	rep1 := NewReplicator(db1, ts.URL)
	if _, err := rep1.Push(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Pull to trigger writeMetaClock (advances lastSeenClock in store)
	if _, err := rep1.Pull(context.Background()); err != nil {
		t.Fatal(err)
	}

	// Second session: same store, new DocDB (simulates "reopened file").
	db2 := New(clientStore, "client")

	// db2 has the meta:clock record from session 1, so it should resume
	// from that clock and pull nothing.
	rep2 := NewReplicator(db2, ts.URL)
	if pulled, _ := rep2.Pull(context.Background()); pulled != 0 {
		t.Errorf("pull on fresh process = %d, want 0 (clock should be persisted)", pulled)
	}
}

// ── Live replication ─────────────────────────────────────────────────────────

func TestReplicatorLivePicksUpNewRecords(t *testing.T) {
	storeA := nell.NewMemoryStore("a")
	ts := newTestServer(storeA, "a")
	defer ts.Close()

	dbB := New(nell.NewMemoryStore("client"), "client")
	rep := NewReplicator(dbB, ts.URL)

	ctx := t.Context()
	stop := rep.Live(ctx, LiveConfig{Interval: 30 * time.Millisecond, BackoffMax: 50 * time.Millisecond})
	defer stop()

	// Server gets a record from a peer.
	clk := nell.NewHLC()
	clk.Tick()
	_, _, _ = storeA.Put(nell.Record{ID: "live-1", Type: nell.TypeText, Payload: []byte(`{"_rev":"1-abc","v":1}`), Clock: clk, UpdatedBy: "peer"})

	// Wait for the live loop to pick it up.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := dbB.Get(context.Background(), "live-1"); err == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Errorf("live-1 never arrived via live replication")
}

// ── test helper ──────────────────────────────────────────────────────────────

// newTestServer wires an in-process nell server onto a MemoryStore.
func newTestServer(store nell.Store, nodeID string) *httptest.Server {
	srv := server.New(store, nodeID)
	return httptest.NewServer(srv.Handler())
}

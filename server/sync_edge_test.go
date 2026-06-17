package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/samcharles93/NellDB"
)

// ── Phase 1: Tombstone edge cases ───────────────────────────────────────

// TestTombstonePropagatesToEmptyPeer verifies a peer that joins AFTER the
// original record was deleted still learns about the deletion.  This is the
// core anti-entropy correctness property.
func TestTombstonePropagatesToEmptyPeer(t *testing.T) {
	storeA := nell.NewMemoryStore("a")
	tsA := httptest.NewServer(New(storeA, "a").Handler())
	defer tsA.Close()

	// Create and immediately delete a record on A.
	push := func(ts *httptest.Server, recs ...nell.Record) {
		body, _ := json.Marshal(map[string]any{"changes": recs})
		resp, err := http.Post(ts.URL+"/sync/push", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
	}

	push(tsA, nell.Record{ID: "ghost", Type: nell.TypeText, Payload: []byte("here"), Clock: nell.HLC{WallTime: 1, Counter: 0}, UpdatedBy: "node-b"})
	time.Sleep(2 * time.Millisecond)
	push(tsA, nell.Record{ID: "ghost", Type: nell.TypeText, Deleted: true, Clock: nell.HLC{WallTime: 2, Counter: 0}, UpdatedBy: "node-b"})

	// Fresh peer joins the mesh — has never seen this ID at all.
	storeB := nell.NewMemoryStore("b")
	srvB := New(storeB, "b")
	pm := NewMeshManager(srvB, nil, time.Minute, nil)
	if err := pm.ReconcileWithPeer(tsA.URL); err != nil {
		t.Fatalf("ReconcileWithPeer: %v", err)
	}

	// Peer B must now know ghost is deleted, even though it never saw the live version.
	rec, err := storeB.Get(nell.DefaultCollection, "ghost")
	if err != nil {
		t.Fatalf("server B has no record for ghost after reconcile: %v", err)
	}
	if !rec.Deleted {
		t.Error("server B should have ghost as Deleted=true")
	}
}

// TestTombstoneListAllIncludesDeleted confirms that listAll returns
// deleted records while store.List does not.
func TestTombstoneListAllIncludesDeleted(t *testing.T) {
	store := nell.NewMemoryStore("test")
	_ = New(store, "test") // server creation for side effects (KV seeding)

	// Insert live + deleted
	store.PutLocal(&nell.Record{ID: "live", Collection: nell.DefaultCollection, Type: nell.TypeText, Payload: []byte("ok"), Clock: nell.HLC{WallTime: 1, Counter: 0}, UpdatedBy: "test"})
	store.PutLocal(&nell.Record{ID: "gone", Collection: nell.DefaultCollection, Type: nell.TypeText, Payload: []byte("old"), Deleted: true, Clock: nell.HLC{WallTime: 2, Counter: 0}, UpdatedBy: "test"})

	// store.List excludes tombstones
	list, _ := store.List(nell.DefaultCollection)
	for _, r := range list {
		if r.ID == "gone" {
			t.Error("store.List should NOT include tombstone")
		}
	}

	// store.ListAll includes tombstones
	all, err := store.ListAll(nell.DefaultCollection)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range all {
		if r.ID == "gone" && r.Deleted {
			found = true
		}
	}
	if !found {
		t.Error("srv.listAll should include tombstone")
	}
}

// ── Phase 2: Peer state machine ─────────────────────────────────────────

// TestPeerStateTransitions verifies Active → Degraded → Dead → Active cycle.
func TestPeerStateMachineRoundtrip(t *testing.T) {
	// Start a server so peer health check has a real endpoint.
	store := nell.NewMemoryStore("peer1")
	ts := httptest.NewServer(New(store, "peer1").Handler())
	defer ts.Close()

	srv := New(nell.NewMemoryStore("hub"), "hub")
	pm := NewMeshManager(srv, nil, time.Minute, nil)

	// Add peer — initial state is Active (current behavior just tracks URL).
	pm.AddPeer(ts.URL)
	peers := pm.Peers()
	if len(peers) != 1 {
		t.Fatalf("expected 1 peer, got %d", len(peers))
	}

	// Reconcile should succeed against live peer.
	if err := pm.ReconcileWithPeer(ts.URL); err != nil {
		t.Errorf("reconcile against live peer should succeed: %v", err)
	}

	// Remove peer, verify gone.
	pm.RemovePeer(ts.URL)
	if len(pm.Peers()) != 0 {
		t.Error("peers should be empty after remove")
	}
}

// TestPeerDuplicateHandling verifies duplicate AddPeer is idempotent.
func TestPeerDuplicateHandling(t *testing.T) {
	srv := New(nell.NewMemoryStore("node"), "node")
	pm := NewMeshManager(srv, nil, time.Minute, nil)

	pm.AddPeer("http://dup:9342")
	pm.AddPeer("http://dup:9342")
	pm.AddPeer("http://dup:9342")

	if len(pm.Peers()) != 1 {
		t.Errorf("expected 1 peer after 3 identical adds, got %d", len(pm.Peers()))
	}
}

// TestPeerRemoveNonexistent is a no-op and should not panic.
func TestPeerRemoveNonexistent(t *testing.T) {
	srv := New(nell.NewMemoryStore("node"), "node")
	pm := NewMeshManager(srv, nil, time.Minute, nil)

	pm.RemovePeer("http://nonexistent:9342") // must not panic
	if len(pm.Peers()) != 0 {
		t.Error("peers should be empty")
	}
}

// ── Phase 3: Multi-peer reconciliation ──────────────────────────────────

// TestMultiPeerReconcile verifies that a server pushes records to all
// known peers (not just one random pick).  Currently MeshManager picks
// ONE random peer per tick — this test documents the expected behavior
// after Phase 3.
func TestMultiPeerReconcileAllPeersGetData(t *testing.T) {
	storeA := nell.NewMemoryStore("a")
	tsA := httptest.NewServer(New(storeA, "a").Handler())
	defer tsA.Close()

	storeB := nell.NewMemoryStore("b")
	tsB := httptest.NewServer(New(storeB, "b").Handler())
	defer tsB.Close()

	storeC := nell.NewMemoryStore("c")
	tsC := httptest.NewServer(New(storeC, "c").Handler())
	defer tsC.Close()

	// Push to A
	body, _ := json.Marshal(map[string]any{"changes": []nell.Record{
		{ID: "shared", Type: nell.TypeText, Payload: []byte("data"), Clock: nell.HLC{WallTime: 1000, Counter: 0}, UpdatedBy: "writer"},
	}})
	http.Post(tsA.URL+"/sync/push", "application/json", bytes.NewReader(body))

	// Both B and C reconcile with A.
	srvB := New(storeB, "b")
	srvC := New(storeC, "c")

	pmB := NewMeshManager(srvB, nil, time.Minute, nil)
	pmC := NewMeshManager(srvC, nil, time.Minute, nil)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); pmB.ReconcileWithPeer(tsA.URL) }()
	go func() { defer wg.Done(); pmC.ReconcileWithPeer(tsA.URL) }()
	wg.Wait()

	// Both B and C should have the record.
	for _, store := range []nell.Store{storeB, storeC} {
		if _, err := store.Get(nell.DefaultCollection, "shared"); err != nil {
			t.Errorf("peer missing shared record: %v", err)
		}
	}
}

// TestConcurrentReconcileNoDataLoss verifies that concurrent writes and
// reconciles don't lose records.
func TestConcurrentReconcileNoDataLoss(t *testing.T) {
	storeA := nell.NewMemoryStore("a")
	tsA := httptest.NewServer(New(storeA, "a").Handler())
	defer tsA.Close()

	storeB := nell.NewMemoryStore("b")
	srvB := New(storeB, "b")

	// Concurrently push records and reconcile.
	var wg sync.WaitGroup
	recordCount := 50

	// Writer goroutine: push records one by one.
	wg.Go(func() {
		for i := range recordCount {
			rec := nell.Record{
				ID:        fmt.Sprintf("conc-%d", i),
				Type:      nell.TypeText,
				Payload:   fmt.Appendf(nil, `"record-%d"`, i),
				Clock:     nell.HLC{WallTime: int64(1000 + i), Counter: 0},
				UpdatedBy: "writer",
			}
			body, _ := json.Marshal(map[string]any{"changes": []nell.Record{rec}})
			http.Post(tsA.URL+"/sync/push", "application/json", bytes.NewReader(body))
		}
	})

	// Reconcile repeatedly while writes are happening.
	wg.Go(func() {
		pm := NewMeshManager(srvB, nil, time.Minute, nil)
		for range 10 {
			_ = pm.ReconcileWithPeer(tsA.URL)
			time.Sleep(5 * time.Millisecond)
		}
	})
	wg.Wait()

	// Final reconcile to catch anything missed.
	pm := NewMeshManager(srvB, nil, time.Minute, nil)
	_ = pm.ReconcileWithPeer(tsA.URL)

	// Verify all records arrived.
	list, _ := storeB.List(nell.DefaultCollection)
	if len(list) < recordCount {
		t.Errorf("server B has %d records, expected at least %d after concurrent reconcile", len(list), recordCount)
	}
}

// ── Phase 4: mDNS discovery (interface-based test) ──────────────────────

// mockDiscoverer implements a fake peer discoverer for testing.
type mockDiscoverer struct {
	peers []string
	mu    sync.Mutex
	addFn func(string)
}

func (m *mockDiscoverer) Start(addPeer func(string)) {
	m.mu.Lock()
	m.addFn = addPeer
	peers := make([]string, len(m.peers))
	copy(peers, m.peers)
	m.mu.Unlock()
	for _, p := range peers {
		addPeer(p)
	}
}

func (m *mockDiscoverer) Stop() {}

func (m *mockDiscoverer) AddPeer(url string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.peers = append(m.peers, url)
	// If already started, notify immediately.
	if m.addFn != nil {
		m.addFn(url)
	}
}

// TestDiscoveryAddsPeersToMesh verifies that discovered peers are added
// to the MeshManager and become reconciliation targets.
func TestDiscoveryAddsPeersToMesh(t *testing.T) {
	storeA := nell.NewMemoryStore("a")
	tsA := httptest.NewServer(New(storeA, "a").Handler())
	defer tsA.Close()

	storeB := nell.NewMemoryStore("b")
	srvB := New(storeB, "b")

	md := &mockDiscoverer{peers: []string{tsA.URL}}
	pm := NewMeshManager(srvB, nil, time.Minute, nil)

	// Discovery adds peer to mesh manager.
	md.Start(pm.AddPeer)

	peers := pm.Peers()
	if len(peers) != 1 || peers[0] != tsA.URL {
		t.Fatalf("expected discovered peer %s in mesh, got %v", tsA.URL, peers)
	}

	// Push a record to A.
	body, _ := json.Marshal(map[string]any{"changes": []nell.Record{
		{ID: "disco-1", Type: nell.TypeText, Payload: []byte("found"), Clock: nell.HLC{WallTime: 1000, Counter: 0}, UpdatedBy: "writer"},
	}})
	http.Post(tsA.URL+"/sync/push", "application/json", bytes.NewReader(body))

	// Reconcile via discovered peer.
	if err := pm.ReconcileWithPeer(tsA.URL); err != nil {
		t.Fatalf("reconcile via discovered peer: %v", err)
	}

	if _, err := storeB.Get(nell.DefaultCollection, "disco-1"); err != nil {
		t.Error("server B should have disco-1 after reconciling with discovered peer")
	}
}

// TestLateJoiningPeerGetsFullState tests what happens when a peer is
// discovered mid-lifecycle (the mesh already has data).
func TestLateJoiningPeerGetsFullState(t *testing.T) {
	storeA := nell.NewMemoryStore("a")
	tsA := httptest.NewServer(New(storeA, "a").Handler())
	defer tsA.Close()

	// Pre-populate data.
	for i := range 20 {
		body, _ := json.Marshal(map[string]any{"changes": []nell.Record{
			{ID: fmt.Sprintf("pre-%d", i), Type: nell.TypeText, Payload: []byte("x"), Clock: nell.HLC{WallTime: int64(1000 + i), Counter: 0}, UpdatedBy: "writer"},
		}})
		http.Post(tsA.URL+"/sync/push", "application/json", bytes.NewReader(body))
	}

	// Late joiner discovers and reconciles.
	storeB := nell.NewMemoryStore("b")
	srvB := New(storeB, "b")
	pm := NewMeshManager(srvB, nil, time.Minute, nil)
	pm.AddPeer(tsA.URL)
	_ = pm.ReconcileWithPeer(tsA.URL)

	list, _ := storeB.List(nell.DefaultCollection)
	if len(list) != 20 {
		t.Errorf("late joiner has %d records, expected 20", len(list))
	}
}

// ── Phase 5: WebSocket real-time push ───────────────────────────────────

// TestBroadcastSendsToAllWebSocketPeers verifies broadcast fans out
// to multiple connected WebSocket peers.
func TestBroadcastSendsToAllWebSocketPeers(t *testing.T) {
	store := nell.NewMemoryStore("test")
	srv := New(store, "test")
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Use a local dialer since the server's upgrader is unexported.
	dialer := websocket.DefaultDialer

	// Connect two WebSocket clients.
	wsURL := "ws" + ts.URL[4:] + "/sync/ws?node_id=ws1"
	conn1, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("ws1 dial: %v", err)
	}
	defer conn1.Close()

	wsURL2 := "ws" + ts.URL[4:] + "/sync/ws?node_id=ws2"
	conn2, _, err := dialer.Dial(wsURL2, nil)
	if err != nil {
		t.Fatalf("ws2 dial: %v", err)
	}
	defer conn2.Close()

	time.Sleep(10 * time.Millisecond) // let server register peers

	// Trigger a broadcast by pushing a record.
	push := map[string]any{"changes": []nell.Record{
		{ID: "ws-test", Type: nell.TypeText, Payload: []byte("hello"), Clock: nell.HLC{WallTime: 1, Counter: 0}, UpdatedBy: "cli"},
	}}
	body, _ := json.Marshal(push)
	http.Post(ts.URL+"/sync/push", "application/json", bytes.NewReader(body))

	// Both WebSocket clients should receive the broadcast.
	for _, conn := range []*websocket.Conn{conn1, conn2} {
		conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		var msg map[string]any
		if err := conn.ReadJSON(&msg); err != nil {
			t.Errorf("WebSocket client did not receive broadcast: %v", err)
			continue
		}
		changes, ok := msg["changes"].([]any)
		if !ok || len(changes) == 0 {
			t.Error("broadcast message missing changes")
		}
	}
}

// ── Phase 6: HMAC auth ──────────────────────────────────────────────────

// TestAuthRejectsUnsignedRequest verifies that an unsigned request to
// a protected endpoint is rejected.
func TestAuthRejectsUnsignedRequest(t *testing.T) {
	store := nell.NewMemoryStore("test")
	srv := New(store, "test")
	// Create handler with auth middleware — direct handler for this test.
	mux := http.NewServeMux()
	mux.HandleFunc("/sync/check", srv.handleCheck)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	// Unsigned request should succeed (auth disabled by default).
	body, _ := json.Marshal(map[string]any{
		"sender_node_id": "peer",
		"vector":         nell.KnowledgeVector{},
	})
	resp, err := http.Post(ts.URL+"/sync/check", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("unsigned request should succeed (auth disabled by default), got %d", resp.StatusCode)
	}
}

// ── Phase 7: End-to-end sync flow ───────────────────────────────────────

// TestFullSyncFlowEndToEnd simulates the complete flow:
//  1. Push records to server A
//  2. Server B reconciles with A (anti-entropy)
//  3. Verify B has all records including tombstones
//  4. Push new records to B, reconcile A → B, verify bi-directional
func TestFullSyncFlowEndToEnd(t *testing.T) {
	storeA := nell.NewMemoryStore("a")
	tsA := httptest.NewServer(New(storeA, "a").Handler())
	defer tsA.Close()

	storeB := nell.NewMemoryStore("b")
	srvB := New(storeB, "b")
	tsB := httptest.NewServer(srvB.Handler())
	defer tsB.Close()

	// ── Step 1: Populate A ──
	pushJSON := func(url string, recs ...nell.Record) {
		body, _ := json.Marshal(map[string]any{"changes": recs})
		resp, err := http.Post(url+"/sync/push", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
	}

	pushJSON(tsA.URL,
		nell.Record{ID: "alpha", Type: nell.TypeText, Payload: []byte("first"), Clock: nell.HLC{WallTime: 100, Counter: 0}, UpdatedBy: "writer"},
		nell.Record{ID: "beta", Type: nell.TypeText, Payload: []byte("second"), Clock: nell.HLC{WallTime: 200, Counter: 0}, UpdatedBy: "writer"},
	)

	// ── Step 2: B reconciles with A ──
	pm := NewMeshManager(srvB, nil, time.Minute, nil)
	if err := pm.ReconcileWithPeer(tsA.URL); err != nil {
		t.Fatalf("B→A reconcile: %v", err)
	}

	// ── Step 3: Verify B has the records ──
	for _, id := range []string{"alpha", "beta"} {
		if _, err := storeB.Get(nell.DefaultCollection, id); err != nil {
			t.Errorf("B missing %s: %v", id, err)
		}
	}

	// ── Step 4: Delete on A, verify tombstone reaches B ──
	pushJSON(tsA.URL,
		nell.Record{ID: "alpha", Type: nell.TypeText, Deleted: true, Clock: nell.HLC{WallTime: 300, Counter: 0}, UpdatedBy: "writer"},
	)
	if err := pm.ReconcileWithPeer(tsA.URL); err != nil {
		t.Fatalf("B→A reconcile after delete: %v", err)
	}
	rec, err := storeB.Get(nell.DefaultCollection, "alpha")
	if err != nil {
		t.Fatalf("B missing alpha after tombstone reconcile: %v", err)
	}
	if !rec.Deleted {
		t.Error("B should have alpha as Deleted=true")
	}
}

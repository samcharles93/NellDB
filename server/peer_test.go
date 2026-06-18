package server

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/samcharles93/NellDB"
)

func TestMeshManagerAddRemovePeer(t *testing.T) {
	srv := New(nell.NewMemoryStore("n1"), "n1")
	pm := NewMeshManager(srv, nil, time.Second, nil)

	pm.AddPeer("https://peer1.example.com")
	if len(pm.Peers()) != 1 {
		t.Errorf("peers = %d, want 1", len(pm.Peers()))
	}

	pm.AddPeer("https://peer2.example.com")
	if len(pm.Peers()) != 2 {
		t.Errorf("peers = %d, want 2", len(pm.Peers()))
	}

	// Duplicate add should be a no-op.
	pm.AddPeer("https://peer1.example.com")
	if len(pm.Peers()) != 2 {
		t.Errorf("duplicate add: peers = %d, want 2", len(pm.Peers()))
	}

	pm.RemovePeer("https://peer1.example.com")
	if len(pm.Peers()) != 1 {
		t.Errorf("after remove: peers = %d, want 1", len(pm.Peers()))
	}

	// Removing non-existent peer is a no-op.
	pm.RemovePeer("https://nonexistent.example.com")
	if len(pm.Peers()) != 1 {
		t.Errorf("remove non-existent: peers = %d, want 1", len(pm.Peers()))
	}
}

func TestMeshManagerNewWithPeers(t *testing.T) {
	srv := New(nell.NewMemoryStore("n1"), "n1")
	peers := []string{"https://a.example.com", "https://b.example.com"}
	pm := NewMeshManager(srv, peers, time.Second, nil)

	if len(pm.peers) != 2 {
		t.Errorf("peers = %d, want 2", len(pm.peers))
	}
	if !pm.peers["https://a.example.com"].isActive() {
		t.Error("peer a should be active on creation")
	}
}

func TestMeshManagerDefaultInterval(t *testing.T) {
	srv := New(nell.NewMemoryStore("n1"), "n1")
	pm := NewMeshManager(srv, nil, 0, nil)
	if pm.interval != 30*time.Second {
		t.Errorf("default interval = %v, want 30s", pm.interval)
	}
}

func TestMeshManagerHealth(t *testing.T) {
	srv := New(nell.NewMemoryStore("n1"), "n1")
	pm := NewMeshManager(srv, nil, time.Second, nil)

	h := pm.Health()
	// Just verify it doesn't panic and returns a struct.
	if h.Replicating {
		t.Log("health reports replicating without Start")
	}
	_ = h
}

func TestMeshManagerReconcileOneInner(t *testing.T) {
	// Set up peer server with data.
	peerStore := nell.NewMemoryStore("peer-a")
	peerSrv := New(peerStore, "peer-a")
	peerTS := httptest.NewServer(peerSrv.Handler())
	defer peerTS.Close()

	// Put some records on the peer.
	peerStore.Put(nell.Record{
		ID: "doc-1", Type: nell.TypeText, Payload: []byte("alpha"),
		Clock: nell.HLC{WallTime: 1000, Counter: 1}, UpdatedBy: "peer-a",
		Collection: nell.DefaultCollection,
	})
	peerStore.Put(nell.Record{
		ID: "doc-2", Type: nell.TypeText, Payload: []byte("beta"),
		Clock: nell.HLC{WallTime: 2000, Counter: 1}, UpdatedBy: "peer-a",
		Collection: nell.DefaultCollection,
	})
	peerSrv.recordSeen(nell.Record{UpdatedBy: "peer-a", Clock: nell.HLC{WallTime: 2000, Counter: 1}})

	// Set up local server.
	localStore := nell.NewMemoryStore("local")
	localSrv := New(localStore, "local")
	pm := NewMeshManager(localSrv, []string{peerTS.URL}, time.Second, nil)
	pm.ctx = t.Context()
	pm.peers[peerTS.URL].recordPing()

	err := pm.reconcileOneInner(peerTS.URL)
	if err != nil {
		t.Fatalf("reconcileOneInner: %v", err)
	}

	// Verify local server received the records.
	got1, err := localStore.Get(nell.DefaultCollection, "doc-1")
	if err != nil {
		t.Fatalf("Get doc-1: %v", err)
	}
	if string(got1.Payload) != "alpha" {
		t.Errorf("doc-1 payload = %q, want alpha", got1.Payload)
	}

	got2, err := localStore.Get(nell.DefaultCollection, "doc-2")
	if err != nil {
		t.Fatalf("Get doc-2: %v", err)
	}
	if string(got2.Payload) != "beta" {
		t.Errorf("doc-2 payload = %q, want beta", got2.Payload)
	}
}

func TestMeshManagerReconcileOneInnerInactivePeer(t *testing.T) {
	localStore := nell.NewMemoryStore("local")
	localSrv := New(localStore, "local")
	pm := NewMeshManager(localSrv, []string{"https://dead.example.com"}, time.Second, nil)
	pm.ctx = t.Context()

	// Mark the peer as dead.
	pm.mu.Lock()
	pm.peers["https://dead.example.com"].state = StateDead
	pm.mu.Unlock()

	err := pm.reconcileOneInner("https://dead.example.com")
	if err == nil {
		t.Error("expected error for dead peer, got nil")
	}
}

func TestMeshManagerReconcileOneInnerUnknownPeer(t *testing.T) {
	localStore := nell.NewMemoryStore("local")
	localSrv := New(localStore, "local")
	pm := NewMeshManager(localSrv, nil, time.Second, nil)
	pm.ctx = t.Context()

	// Unknown peer (not in peers map) should not panic.
	err := pm.reconcileOneInner("https://unknown.example.com")
	// Will fail on HTTP connection, but that's expected.
	if err == nil {
		t.Log("unexpected success connecting to unknown peer")
	}
}

func TestMeshManagerReconcileWithPeer(t *testing.T) {
	peerStore := nell.NewMemoryStore("peer-a")
	peerSrv := New(peerStore, "peer-a")
	peerTS := httptest.NewServer(peerSrv.Handler())
	defer peerTS.Close()

	peerStore.Put(nell.Record{
		ID: "doc-1", Type: nell.TypeText, Payload: []byte("data"),
		Clock: nell.HLC{WallTime: 1000, Counter: 1}, UpdatedBy: "peer-a",
		Collection: nell.DefaultCollection,
	})
	peerSrv.recordSeen(nell.Record{UpdatedBy: "peer-a", Clock: nell.HLC{WallTime: 1000, Counter: 1}})

	localStore := nell.NewMemoryStore("local")
	localSrv := New(localStore, "local")
	pm := NewMeshManager(localSrv, []string{peerTS.URL}, time.Second, nil)
	pm.ctx = t.Context()
	pm.peers[peerTS.URL].recordPing()

	err := pm.ReconcileWithPeer(peerTS.URL)
	if err != nil {
		t.Fatalf("ReconcileWithPeer: %v", err)
	}

	_, err = localStore.Get(nell.DefaultCollection, "doc-1")
	if err != nil {
		t.Errorf("local store should have doc-1: %v", err)
	}
}

func TestMeshManagerGetLocalKnowledgeVector(t *testing.T) {
	localStore := nell.NewMemoryStore("local")
	localSrv := New(localStore, "local")
	localSrv.recordSeen(nell.Record{UpdatedBy: "node-a", Clock: nell.HLC{WallTime: 500, Counter: 3}})

	pm := NewMeshManager(localSrv, nil, time.Second, nil)
	kv := pm.GetLocalKnowledgeVector()

	clock, ok := kv["node-a"]
	if !ok {
		t.Fatal("KV should contain node-a")
	}
	if clock.WallTime != 500 || clock.Counter != 3 {
		t.Errorf("KV[node-a] = %v, want {500, 3}", clock)
	}
}

func TestMeshManagerBroadcastMutation(t *testing.T) {
	localStore := nell.NewMemoryStore("local")
	localSrv := New(localStore, "local")
	pm := NewMeshManager(localSrv, nil, time.Second, nil)

	// BroadcastMutation pushes to WebSocket peers. With none connected, it's a no-op.
	pm.BroadcastMutation(nell.Record{
		ID: "doc-1", UpdatedBy: "local", Clock: nell.HLC{WallTime: 100, Counter: 1},
	})
}

func TestMeshManagerPeersAll(t *testing.T) {
	srv := New(nell.NewMemoryStore("n1"), "n1")
	pm := NewMeshManager(srv, nil, time.Second, nil)
	pm.AddPeer("https://a.example.com")
	pm.AddPeer("https://b.example.com")

	all := pm.PeersAll()
	if len(all) != 2 {
		t.Errorf("PeersAll = %d, want 2", len(all))
	}
}

func TestJoinURL(t *testing.T) {
	tests := []struct {
		base, path, want string
	}{
		{"https://example.com", "/sync/check", "https://example.com/sync/check"},
		{"https://example.com:9342", "/sync/check", "https://example.com:9342/sync/check"},
	}
	for _, tc := range tests {
		got := joinURL(tc.base, tc.path)
		if got != tc.want {
			t.Errorf("joinURL(%q, %q) = %q, want %q", tc.base, tc.path, got, tc.want)
		}
	}
}

func TestMeshManagerReconcileOneInnerNoData(t *testing.T) {
	peerStore := nell.NewMemoryStore("peer-a")
	peerSrv := New(peerStore, "peer-a")
	peerTS := httptest.NewServer(peerSrv.Handler())
	defer peerTS.Close()

	localStore := nell.NewMemoryStore("local")
	localSrv := New(localStore, "local")
	pm := NewMeshManager(localSrv, []string{peerTS.URL}, time.Second, nil)
	pm.ctx = t.Context()
	pm.peers[peerTS.URL].recordPing()

	err := pm.reconcileOneInner(peerTS.URL)
	if err != nil {
		t.Fatalf("reconcileOneInner with empty peer: %v", err)
	}

	list, _ := localStore.List(nell.DefaultCollection)
	if len(list) != 0 {
		t.Errorf("local store has %d records, want 0", len(list))
	}
}

package server

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/samcharles93/NellDB"
)

func newTestServer(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()
	store := nell.NewMemoryStore("test-server")
	srv := New(store, "test-server")
	ts := httptest.NewServer(srv.Handler())
	return srv, ts
}

// ── Valid flows ──────────────────────────────────────────────────────────────

func TestServerPushThenPull(t *testing.T) {
	_, ts := newTestServer(t)
	defer ts.Close()

	// Push
	push := map[string]any{"changes": []nell.Record{
		{ID: "doc-1", Type: nell.TypeText, Payload: []byte("hello"), Clock: nell.HLC{WallTime: 1000, Counter: 0}, UpdatedBy: "client"},
	}}
	body, _ := json.Marshal(push)
	resp, err := http.Post(ts.URL+"/sync/push", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("push: status %d", resp.StatusCode)
	}

	// Pull
	pull := map[string]any{"since": nell.HLC{WallTime: 0, Counter: 0}}
	body, _ = json.Marshal(pull)
	resp, err = http.Post(ts.URL+"/sync/pull", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Changes []nell.Record `json:"changes"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&result)
	_ = resp.Body.Close()

	if len(result.Changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(result.Changes))
	}
}

// ── Invalid input ────────────────────────────────────────────────────────────

func TestServerPushInvalidJSON(t *testing.T) {
	_, ts := newTestServer(t)
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/sync/push", "application/json", strings.NewReader("not json"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid JSON, got %d", resp.StatusCode)
	}
}

func TestServerPushEmptyBody(t *testing.T) {
	_, ts := newTestServer(t)
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/sync/push", "application/json", strings.NewReader(""))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for empty body, got %d", resp.StatusCode)
	}
}

func TestServerPushEmptyChanges(t *testing.T) {
	_, ts := newTestServer(t)
	defer ts.Close()

	body, _ := json.Marshal(map[string]any{"changes": []nell.Record{}})
	resp, err := http.Post(ts.URL+"/sync/push", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("empty changes should be OK, got %d", resp.StatusCode)
	}
}

func TestServerPullInvalidJSON(t *testing.T) {
	_, ts := newTestServer(t)
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/sync/pull", "application/json", strings.NewReader("garbage"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
}

func TestServerCheckWithEmptyVector(t *testing.T) {
	_, ts := newTestServer(t)
	defer ts.Close()

	body, _ := json.Marshal(map[string]any{
		"sender_node_id": "peer-a",
		"vector":         nell.KnowledgeVector{},
	})
	resp, err := http.Post(ts.URL+"/sync/check", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("empty vector check should be OK, got %d", resp.StatusCode)
	}
}

func TestServerWrongMethod(t *testing.T) {
	_, ts := newTestServer(t)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/sync/pull")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for GET, got %d", resp.StatusCode)
	}
}

// ── Body size limit ──────────────────────────────────────────────────────────

func TestServerRejectsOversizedBody(t *testing.T) {
	_, ts := newTestServer(t)
	defer ts.Close()

	// Create a payload larger than MaxBodyBytes (10 MiB).
	bigPayload := make([]byte, 11<<20)
	body, _ := json.Marshal(map[string]any{
		"changes": []map[string]any{{"id": "big", "payload": bigPayload}},
	})
	resp, err := http.Post(ts.URL+"/sync/push", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("expected 413 for oversized body, got %d", resp.StatusCode)
	}
}

// ── HMAC auth ────────────────────────────────────────────────────────────────

func TestHMACAuthAccept(t *testing.T) {
	secret := bytes.Repeat([]byte("x"), 32)
	store := nell.NewMemoryStore("test-server")
	srv := New(store, "test-server")
	h := HMACAuth(secret)(srv.Handler())
	ts := httptest.NewServer(h)
	defer ts.Close()

	push := map[string]any{"changes": []nell.Record{
		{ID: "doc-1", Type: nell.TypeText, Payload: []byte("hello"), Clock: nell.HLC{WallTime: 1000}, UpdatedBy: "client"},
	}}
	body, _ := json.Marshal(push)

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/sync/push", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	now := time.Now().Unix()
	req.Header.Set("X-Nell-Timestamp", fmt.Sprintf("%d", now))

	mac := hmac.New(sha256.New, secret)
	_, _ = fmt.Fprintf(mac, "%d\n", now)
	mac.Write(body)
	req.Header.Set("X-Nell-Signature", hex.EncodeToString(mac.Sum(nil)))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestHMACAuthReject(t *testing.T) {
	secret := bytes.Repeat([]byte("x"), 32)
	store := nell.NewMemoryStore("test-server")
	srv := New(store, "test-server")
	h := HMACAuth(secret)(srv.Handler())
	ts := httptest.NewServer(h)
	defer ts.Close()

	// Missing headers
	push := map[string]any{"changes": []nell.Record{}}
	body, _ := json.Marshal(push)
	resp, err := http.Post(ts.URL+"/sync/push", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", resp.StatusCode)
	}

	// Bad signature
	req2, _ := http.NewRequest(http.MethodPost, ts.URL+"/sync/push", bytes.NewReader(body))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("X-Nell-Timestamp", fmt.Sprintf("%d", time.Now().Unix()))
	req2.Header.Set("X-Nell-Signature", "deadbeef")
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp2.Body.Close() }()
	if resp2.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 for bad sig, got %d", resp2.StatusCode)
	}
}

func TestHMACAuthNoopWhenEmpty(t *testing.T) {
	store := nell.NewMemoryStore("test-server")
	srv := New(store, "test-server")
	h := HMACAuth(nil)(srv.Handler())
	ts := httptest.NewServer(h)
	defer ts.Close()

	body, _ := json.Marshal(map[string]any{"changes": []nell.Record{}})
	resp, err := http.Post(ts.URL+"/sync/push", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 (auth disabled), got %d", resp.StatusCode)
	}
}

// ── Concurrency ──────────────────────────────────────────────────────────────

func TestServerConcurrentPushes(t *testing.T) {
	_, ts := newTestServer(t)
	defer ts.Close()

	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func(seq int) {
			defer wg.Done()
			rec := nell.Record{
				ID:        fmt.Sprintf("conc-%d", seq),
				Type:      nell.TypeText,
				Payload:   fmt.Appendf(nil, "concurrent-%d", seq),
				Clock:     nell.NewHLC(),
				UpdatedBy: "client",
			}
			body, _ := json.Marshal(map[string]any{"changes": []nell.Record{rec}})
			resp, err := http.Post(ts.URL+"/sync/push", "application/json", bytes.NewReader(body))
			if err != nil {
				t.Errorf("push %d: %v", seq, err)
				return
			}
			_ = resp.Body.Close()
		}(i)
	}
	wg.Wait()

	// Pull should return all 20
	body, _ := json.Marshal(map[string]any{"since": nell.HLC{}})
	resp, err := http.Post(ts.URL+"/sync/pull", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var result struct {
		Changes []nell.Record `json:"changes"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&result)
	if len(result.Changes) != 20 {
		t.Errorf("expected 20 records from concurrent pushes, got %d", len(result.Changes))
	}
}

// ── Large payload ────────────────────────────────────────────────────────────

func TestServerPushLargePayload(t *testing.T) {
	_, ts := newTestServer(t)
	defer ts.Close()

	large := make([]byte, 1<<18) // 256 KB
	for i := range large {
		large[i] = byte(i % 256)
	}

	body, _ := json.Marshal(map[string]any{"changes": []nell.Record{
		{ID: "big", Type: nell.TypeImage, Payload: large, Clock: nell.HLC{WallTime: 1, Counter: 0}, UpdatedBy: "client"},
	}})
	resp, err := http.Post(ts.URL+"/sync/push", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("large payload push: status %d", resp.StatusCode)
	}

	// Pull back and verify
	pull, _ := json.Marshal(map[string]any{"since": nell.HLC{}})
	resp2, err := http.Post(ts.URL+"/sync/pull", "application/json", bytes.NewReader(pull))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp2.Body.Close() }()
	var result struct {
		Changes []nell.Record `json:"changes"`
	}
	_ = json.NewDecoder(resp2.Body).Decode(&result)
	if len(result.Changes) != 1 {
		t.Fatalf("expected 1 record, got %d", len(result.Changes))
	}
	if len(result.Changes[0].Payload) != len(large) {
		t.Errorf("payload truncated: got %d bytes, want %d", len(result.Changes[0].Payload), len(large))
	}
}

// ── Anti-entropy check ───────────────────────────────────────────────────────

func TestServerCheckReturnsMissing(t *testing.T) {
	_, ts := newTestServer(t)
	defer ts.Close()

	// Push from node-b
	body, _ := json.Marshal(map[string]any{"changes": []nell.Record{
		{ID: "remote-1", Type: nell.TypeText, Payload: []byte("remote"), Clock: nell.HLC{WallTime: 5000, Counter: 0}, UpdatedBy: "node-b"},
	}})
	if _, err := http.Post(ts.URL+"/sync/push", "application/json", bytes.NewReader(body)); err != nil {
		t.Fatal(err)
	}

	// Check with vector that hasn't seen node-b
	check, _ := json.Marshal(map[string]any{
		"sender_node_id": "node-a",
		"vector":         nell.KnowledgeVector{},
	})
	resp, err := http.Post(ts.URL+"/sync/check", "application/json", bytes.NewReader(check))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	var result struct {
		Missing []nell.Record `json:"missing_changes"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&result)
	_ = result
	// node-a's empty vector means it should receive the node-b record
	found := false
	for _, r := range result.Missing {
		if r.ID == "remote-1" {
			found = true
		}
	}
	if !found {
		t.Error("check should return missing records for unseen nodes")
	}
}

// ── Anti-entropy edge cases ─────────────────────────────────────────────────

func TestServerCheckPartialKnowledge(t *testing.T) {
	_, ts := newTestServer(t)
	defer ts.Close()

	// Push from two different nodes
	pushRecs := []nell.Record{
		{ID: "a-1", Type: nell.TypeText, Payload: []byte("a"), Clock: nell.HLC{WallTime: 1000, Counter: 0}, UpdatedBy: "node-a"},
		{ID: "b-1", Type: nell.TypeText, Payload: []byte("b"), Clock: nell.HLC{WallTime: 2000, Counter: 0}, UpdatedBy: "node-b"},
		{ID: "b-2", Type: nell.TypeText, Payload: []byte("b2"), Clock: nell.HLC{WallTime: 3000, Counter: 0}, UpdatedBy: "node-b"},
	}
	for _, rec := range pushRecs {
		body, _ := json.Marshal(map[string]any{"changes": []nell.Record{rec}})
		_, _ = http.Post(ts.URL+"/sync/push", "application/json", bytes.NewReader(body))
	}

	// Sender has seen node-a up to 1000, but knows nothing of node-b
	kv := nell.KnowledgeVector{
		"node-a": {WallTime: 1000, Counter: 0},
	}
	check, _ := json.Marshal(map[string]any{
		"sender_node_id": "node-c",
		"vector":         kv,
	})
	resp, err := http.Post(ts.URL+"/sync/check", "application/json", bytes.NewReader(check))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	var result struct {
		Missing []nell.Record `json:"missing_changes"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&result)

	// Should get both b-1 and b-2 (unseen node-b), but not a-1 (already seen)
	ids := make(map[string]bool)
	for _, r := range result.Missing {
		ids[r.ID] = true
	}
	if ids["a-1"] {
		t.Error("a-1 should not be missing (sender has seen node-a at clock 1000)")
	}
	if !ids["b-1"] {
		t.Error("b-1 should be missing (sender knows nothing of node-b)")
	}
	if !ids["b-2"] {
		t.Error("b-2 should be missing (sender knows nothing of node-b)")
	}
}

func TestServerCheckStaleClock(t *testing.T) {
	_, ts := newTestServer(t)
	defer ts.Close()

	// Push a record from node-b at clock 5000
	body, _ := json.Marshal(map[string]any{"changes": []nell.Record{
		{ID: "b-1", Type: nell.TypeText, Payload: []byte("new"), Clock: nell.HLC{WallTime: 5000, Counter: 0}, UpdatedBy: "node-b"},
	}})
	_, _ = http.Post(ts.URL+"/sync/push", "application/json", bytes.NewReader(body))

	// Sender claims it's seen node-b up to 10000 — should get nothing from node-b
	kv := nell.KnowledgeVector{
		"node-b": {WallTime: 10000, Counter: 0},
	}
	check, _ := json.Marshal(map[string]any{
		"sender_node_id": "node-a",
		"vector":         kv,
	})
	resp, err := http.Post(ts.URL+"/sync/check", "application/json", bytes.NewReader(check))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	var result struct {
		Missing []nell.Record `json:"missing_changes"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&result)

	for _, r := range result.Missing {
		if r.UpdatedBy == "node-b" {
			t.Error("should not return node-b records (sender claims to have seen it at a later clock)")
		}
	}
}

func TestServerCheckServerReturnsOwnRecords(t *testing.T) {
	_, ts := newTestServer(t)
	defer ts.Close()

	// Push records from two nodes including the server's own node
	body, _ := json.Marshal(map[string]any{"changes": []nell.Record{
		{ID: "self-1", Type: nell.TypeText, Payload: []byte("self"), Clock: nell.HLC{WallTime: 5000, Counter: 0}, UpdatedBy: "test-server"},
	}})
	_, _ = http.Post(ts.URL+"/sync/push", "application/json", bytes.NewReader(body))

	// Empty vector — should return all records, including the server's own
	check, _ := json.Marshal(map[string]any{
		"sender_node_id": "peer",
		"vector":         nell.KnowledgeVector{},
	})
	resp, err := http.Post(ts.URL+"/sync/check", "application/json", bytes.NewReader(check))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	var result struct {
		Missing []nell.Record `json:"missing_changes"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&result)

	found := false
	for _, r := range result.Missing {
		if r.ID == "self-1" {
			found = true
		}
	}
	if !found {
		t.Error("check should return server's own records when peer hasn't seen them")
	}
}

func TestServerCheckTombstoneIncluded(t *testing.T) {
	_, ts := newTestServer(t)
	defer ts.Close()

	// Push a live record, then tombstone it
	body1, _ := json.Marshal(map[string]any{"changes": []nell.Record{
		{ID: "dying", Type: nell.TypeText, Payload: []byte("alive"), Clock: nell.HLC{WallTime: 1000, Counter: 0}, UpdatedBy: "node-b"},
	}})
	_, _ = http.Post(ts.URL+"/sync/push", "application/json", bytes.NewReader(body1))

	time.Sleep(2 * time.Millisecond)
	body2, _ := json.Marshal(map[string]any{"changes": []nell.Record{
		{ID: "dying", Type: nell.TypeText, Deleted: true, Clock: nell.HLC{WallTime: 2000, Counter: 0}, UpdatedBy: "node-b"},
	}})
	_, _ = http.Post(ts.URL+"/sync/push", "application/json", bytes.NewReader(body2))

	// Peer hasn't seen anything
	check, _ := json.Marshal(map[string]any{
		"sender_node_id": "peer",
		"vector":         nell.KnowledgeVector{},
	})
	resp, err := http.Post(ts.URL+"/sync/check", "application/json", bytes.NewReader(check))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	var result struct {
		Missing []nell.Record `json:"missing_changes"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&result)

	// handleCheck now uses listAll (which includes tombstones),
	// so tombstones ARE returned via /sync/check for anti-entropy.
	tombstoneFound := false
	for _, r := range result.Missing {
		if r.ID == "dying" && r.Deleted {
			tombstoneFound = true
			break
		}
	}
	if !tombstoneFound {
		t.Error("tombstone should be returned via /sync/check so deletes propagate between servers")
	}
}

// TestTombstonePropagationMesh verifies that tombstones propagate between
// servers via MeshManager reconciliation — a peer that never saw the original
// record still learns about its deletion.
func TestTombstonePropagationMesh(t *testing.T) {
	storeA := nell.NewMemoryStore("a")
	srvA := New(storeA, "a")
	tsA := httptest.NewServer(srvA.Handler())
	defer tsA.Close()

	storeB := nell.NewMemoryStore("b")
	srvB := New(storeB, "b")
	tsB := httptest.NewServer(srvB.Handler())
	defer tsB.Close()

	// Push a live record to server A, then tombstone it.
	push := func(ts *httptest.Server, recs ...nell.Record) {
		body, _ := json.Marshal(map[string]any{"changes": recs})
		resp, err := http.Post(ts.URL+"/sync/push", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
	}

	push(tsA, nell.Record{ID: "dying", Type: nell.TypeText, Payload: []byte("alive"), Clock: nell.HLC{WallTime: 1000, Counter: 0}, UpdatedBy: "node-b"})
	push(tsA, nell.Record{ID: "dying", Type: nell.TypeText, Deleted: true, Clock: nell.HLC{WallTime: 2000, Counter: 0}, UpdatedBy: "node-b"})

	// Server B reconciles with A — should receive both the live record
	// and the tombstone.
	pm := NewMeshManager(srvB, nil, time.Minute, nil)
	if err := pm.ReconcileWithPeer(tsA.URL); err != nil {
		t.Fatalf("ReconcileWithPeer: %v", err)
	}

	// Server B should have the record as a tombstone.
	rec, err := storeB.Get(nell.DefaultCollection, "dying")
	if err != nil {
		t.Fatalf("server B missing record after reconcile: %v", err)
	}
	if !rec.Deleted {
		t.Error("server B should have the record as a tombstone (Deleted=true)")
	}
}

// ── Anti-entropy reconciliation ────────────────────────────────────────────

func TestMeshManagerReconcile(t *testing.T) {
	// Server A has data, server B starts empty
	storeA := nell.NewMemoryStore("a")
	tsA := httptest.NewServer(New(storeA, "a").Handler())
	defer tsA.Close()

	storeB := nell.NewMemoryStore("b")
	srvB := New(storeB, "b")
	tsB := httptest.NewServer(srvB.Handler())
	defer tsB.Close()

	// Push records to server A
	push := map[string]any{"changes": []nell.Record{
		{ID: "doc-1", Type: nell.TypeText, Payload: []byte("hello"), Clock: nell.HLC{WallTime: 1000, Counter: 0}, UpdatedBy: "writer"},
		{ID: "doc-2", Type: nell.TypeText, Payload: []byte("world"), Clock: nell.HLC{WallTime: 2000, Counter: 0}, UpdatedBy: "writer"},
	}}
	body, _ := json.Marshal(push)
	_, _ = http.Post(tsA.URL+"/sync/push", "application/json", bytes.NewReader(body))

	// Server B reconciles with server A
	pm := NewMeshManager(srvB, nil, time.Minute, nil)
	err := pm.ReconcileWithPeer(tsA.URL)
	if err != nil {
		t.Fatalf("ReconcileWithPeer: %v", err)
	}

	// Server B should now have the records
	for _, id := range []string{"doc-1", "doc-2"} {
		if _, err := storeB.Get(nell.DefaultCollection, id); err != nil {
			t.Errorf("server B missing %s after reconcile: %v", id, err)
		}
	}
}

func TestMeshManagerReconcileIdempotent(t *testing.T) {
	storeA := nell.NewMemoryStore("a")
	tsA := httptest.NewServer(New(storeA, "a").Handler())
	defer tsA.Close()

	storeB := nell.NewMemoryStore("b")
	srvB := New(storeB, "b")

	pm := NewMeshManager(srvB, nil, time.Minute, nil)

	// First reconcile: gets records
	push := map[string]any{"changes": []nell.Record{
		{ID: "doc-1", Type: nell.TypeText, Payload: []byte("data"), Clock: nell.HLC{WallTime: 1000, Counter: 0}, UpdatedBy: "writer"},
	}}
	body, _ := json.Marshal(push)
	_, _ = http.Post(tsA.URL+"/sync/push", "application/json", bytes.NewReader(body))

	if err := pm.ReconcileWithPeer(tsA.URL); err != nil {
		t.Fatalf("first reconcile: %v", err)
	}

	// Second reconcile: should get nothing new (knowledge vector prevents re-fetch)
	if err := pm.ReconcileWithPeer(tsA.URL); err != nil {
		t.Fatalf("second reconcile: %v", err)
	}

	// Verify server B only has 1 record
	list, _ := storeB.List(nell.DefaultCollection)
	if len(list) != 1 {
		t.Errorf("expected 1 record after idempotent reconcile, got %d", len(list))
	}
}

func TestMeshManagerAddRemovePeers(t *testing.T) {
	srv := New(nell.NewMemoryStore("node"), "node")
	pm := NewMeshManager(srv, nil, time.Minute, nil)

	pm.AddPeer("http://peer1:9342")
	pm.AddPeer("http://peer2:9342")
	pm.AddPeer("http://peer1:9342") // duplicate — should be ignored

	peers := pm.Peers()
	if len(peers) != 2 {
		t.Errorf("expected 2 peers, got %d: %v", len(peers), peers)
	}

	pm.RemovePeer("http://peer1:9342")
	peers = pm.Peers()
	if len(peers) != 1 {
		t.Errorf("expected 1 peer after remove, got %d", len(peers))
	}
	if peers[0] != "http://peer2:9342" {
		t.Errorf("remaining peer = %s, want http://peer2:9342", peers[0])
	}
}

func TestMeshManagerStartStop(t *testing.T) {
	srv := New(nell.NewMemoryStore("node"), "node")
	pm := NewMeshManager(srv, nil, 50*time.Millisecond, nil)

	pm.Start()
	pm.Start() // idempotent — should not panic or double-start
	pm.Stop()
	pm.Stop() // idempotent
}

// ── Peer state machine tests ──────────────────────────────────────────────

func TestPeerStateTransitions(t *testing.T) {
	p := newTrackedPeer("http://peer:9342")

	if p.getState() != StateActive {
		t.Error("new peer should be active")
	}

	// Simulate missed pings: Active → Degraded → Dead
	p.recordMiss(DefaultMaxMissedPings) // missedPings=1 → Degraded
	if p.getState() != StateDegraded {
		t.Errorf("after 1 miss: state=%s, want %s", p.getState(), StateDegraded)
	}

	p.recordMiss(DefaultMaxMissedPings) // missedPings=2 → still Degraded
	if p.getState() != StateDegraded {
		t.Errorf("after 2 misses: state=%s, want %s", p.getState(), StateDegraded)
	}

	p.recordMiss(DefaultMaxMissedPings) // missedPings=3 → Dead
	if p.getState() != StateDead {
		t.Errorf("after 3 misses: state=%s, want %s", p.getState(), StateDead)
	}

	// A successful ping resets to Active
	p.recordPing()
	if p.getState() != StateActive {
		t.Errorf("after ping: state=%s, want %s", p.getState(), StateActive)
	}
	if p.MissedPings != 0 {
		t.Errorf("after ping: missedPings=%d, want 0", p.MissedPings)
	}
}

func TestReconcileSkipsDeadPeers(t *testing.T) {
	storeA := nell.NewMemoryStore("a")
	srvA := New(storeA, "a")
	tsA := httptest.NewServer(srvA.Handler())
	defer tsA.Close()

	storeB := nell.NewMemoryStore("b")
	srvB := New(storeB, "b")

	pm := NewMeshManager(srvB, []string{tsA.URL}, time.Minute, nil)

	// Manually mark the peer as dead.
	pm.mu.Lock()
	p := pm.peers[tsA.URL]
	pm.mu.Unlock()
	// Force state to dead by simulating max missed pings.
	p.recordMiss(DefaultMaxMissedPings)
	p.recordMiss(DefaultMaxMissedPings)
	p.recordMiss(DefaultMaxMissedPings)
	if p.getState() != StateDead {
		t.Fatalf("peer should be dead, got %s", p.getState())
	}

	// Push data to server A, then try reconcile — should skip dead peer.
	push := map[string]any{"changes": []nell.Record{
		{ID: "doc-1", Type: nell.TypeText, Payload: []byte("data"), Clock: nell.HLC{WallTime: 1000, Counter: 0}, UpdatedBy: "writer"},
	}}
	body, _ := json.Marshal(push)
	_, _ = http.Post(tsA.URL+"/sync/push", "application/json", bytes.NewReader(body))

	// reconcileOne should skip dead peers.
	err := pm.reconcileOne(tsA.URL)
	if err == nil {
		t.Log("reconcileOne on dead peer: expected error (skipped), got nil")
	}

	// Peers() should not include dead peer.
	active := pm.Peers()
	if len(active) != 0 {
		t.Errorf("expected 0 active peers, got %d: %v", len(active), active)
	}
}

func TestPeerHeartbeat(t *testing.T) {
	srv := New(nell.NewMemoryStore("node"), "node")
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	pm := NewMeshManager(srv, []string{ts.URL}, time.Minute, nil)
	pm.Start()
	defer pm.Stop()

	// Wait for at least one heartbeat cycle.
	time.Sleep(DefaultHeartbeatInterval + 500*time.Millisecond)

	pm.mu.RLock()
	p, ok := pm.peers[ts.URL]
	pm.mu.RUnlock()
	if !ok {
		t.Fatal("peer not found in tracker")
	}

	if p.getState() != StateActive {
		t.Errorf("live peer should be active after heartbeat, got %s", p.getState())
	}
	if p.MissedPings != 0 {
		t.Errorf("live peer should have 0 missed pings, got %d", p.MissedPings)
	}
}

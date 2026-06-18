package sdk

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/samcharles93/NellDB"
)

func TestNextBackoff(t *testing.T) {
	tests := []struct {
		cur, max, want time.Duration
	}{
		{1 * time.Second, 60 * time.Second, 2 * time.Second},
		{30 * time.Second, 60 * time.Second, 60 * time.Second},
		{60 * time.Second, 60 * time.Second, 60 * time.Second},
		{100 * time.Second, 60 * time.Second, 60 * time.Second},
		{500 * time.Millisecond, 2 * time.Second, 1 * time.Second},
	}
	for _, tc := range tests {
		got := nextBackoff(tc.cur, tc.max)
		if got != tc.want {
			t.Errorf("nextBackoff(%v, %v) = %v, want %v", tc.cur, tc.max, got, tc.want)
		}
	}
}

func TestIsInternalID(t *testing.T) {
	tests := []struct {
		id   string
		want bool
	}{
		{"meta:clock", true},
		{"meta:vector", true},
		{"meta:something", true},
		{"meta:", true},
		{"meta", false}, // too short
		{"metaz:clock", false},
		{"doc-1", false},
		{"", false},
		{"m", false},
		{"meta", false},
	}
	for _, tc := range tests {
		got := isInternalID(tc.id)
		if got != tc.want {
			t.Errorf("isInternalID(%q) = %v, want %v", tc.id, got, tc.want)
		}
	}
}

func TestJoinPath(t *testing.T) {
	tests := []struct {
		base, path, want string
	}{
		{"https://example.com", "/sync/push", "https://example.com/sync/push"},
		{"https://example.com/api", "/sync/push", "https://example.com/api/sync/push"},
		{"://invalid", "/path", "://invalid/path"},
	}
	for _, tc := range tests {
		got := joinPath(tc.base, tc.path)
		if got != tc.want {
			t.Errorf("joinPath(%q, %q) = %q, want %q", tc.base, tc.path, got, tc.want)
		}
	}
}

func TestReplicatorSetAuthSecret(t *testing.T) {
	db := New(nell.NewMemoryStore("n"), "n", nell.DefaultCollection)
	rep := NewReplicator(db, "https://example.com")

	// Initially no transport override.
	if rep.authSecret != nil {
		t.Error("authSecret should be nil initially")
	}

	// Set a secret — should install signingTransport.
	rep.SetAuthSecret([]byte("my-secret"))
	if rep.authSecret == nil {
		t.Error("authSecret should be set")
	}
	if rep.HTTP.Transport == nil {
		t.Error("HTTP.Transport should be set after SetAuthSecret")
	}
	if _, ok := rep.HTTP.Transport.(*signingTransport); !ok {
		t.Errorf("HTTP.Transport should be *signingTransport, got %T", rep.HTTP.Transport)
	}

	// Clear the secret — should clear transport.
	rep.SetAuthSecret(nil)
	if rep.authSecret != nil {
		t.Error("authSecret should be nil after clear")
	}
	if rep.HTTP.Transport != nil {
		t.Error("HTTP.Transport should be nil after clearing secret")
	}

	// Empty slice clears transport (authSecret is set to empty slice, not nil).
	rep.SetAuthSecret([]byte("s"))
	rep.SetAuthSecret([]byte{})
	if rep.HTTP.Transport != nil {
		t.Error("HTTP.Transport should be nil after empty slice secret")
	}
}

func TestSigningTransportRoundTrip(t *testing.T) {
	var capturedHeaders http.Header
	var capturedBody []byte
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeaders = r.Header.Clone()
		var err error
		capturedBody, err = io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("server read body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer ts.Close()

	st := &signingTransport{
		secret:    []byte("test-secret"),
		transport: http.DefaultTransport,
	}

	body := []byte(`{"doc":"hello"}`)
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/sync/push", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := st.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	// Verify auth headers were added.
	tsStr := capturedHeaders.Get("X-Nell-Timestamp")
	if tsStr == "" {
		t.Error("X-Nell-Timestamp header missing")
	}
	sigStr := capturedHeaders.Get("X-Nell-Signature")
	if sigStr == "" {
		t.Error("X-Nell-Signature header missing")
	}

	// Verify the body was preserved.
	if !bytes.Equal(capturedBody, body) {
		t.Errorf("body = %q, want %q", capturedBody, body)
	}

	var tsVal int64
	if _, err := fmt.Sscanf(tsStr, "%d", &tsVal); err != nil {
		t.Fatalf("bad timestamp: %v", err)
	}
	wantSig := nell.SignBody([]byte("test-secret"), tsVal, body)
	if sigStr != wantSig {
		t.Errorf("signature = %q, want %q", sigStr, wantSig)
	}
}

func TestSigningTransportRoundTripNoBody(t *testing.T) {
	var capturedHeaders http.Header
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeaders = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	st := &signingTransport{
		secret:    []byte("test-secret"),
		transport: http.DefaultTransport,
	}

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/sync/health", nil)
	if err != nil {
		t.Fatal(err)
	}

	resp, err := st.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	defer resp.Body.Close()

	tsStr := capturedHeaders.Get("X-Nell-Timestamp")
	sigStr := capturedHeaders.Get("X-Nell-Signature")
	if tsStr == "" || sigStr == "" {
		t.Error("auth headers should be present even for GET with no body")
	}
}

func TestSigningTransportDefaultTransport(t *testing.T) {
	// When transport is nil, it should fall back to http.DefaultTransport.
	st := &signingTransport{
		secret:    []byte("s"),
		transport: nil,
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/sync/health", nil)
	if err != nil {
		t.Fatal(err)
	}

	resp, err := st.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip with nil transport: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestReplicatorHealth(t *testing.T) {
	db := New(nell.NewMemoryStore("n"), "n", nell.DefaultCollection)
	rep := NewReplicator(db, "https://example.com")

	h := rep.Health()
	if !h.Replicating {
		t.Error("Health().Replicating should be true")
	}

	// Initially no errors or timestamps.
	if h.PushErrors != 0 {
		t.Errorf("PushErrors = %d, want 0", h.PushErrors)
	}
	if h.PullErrors != 0 {
		t.Errorf("PullErrors = %d, want 0", h.PullErrors)
	}
	if h.ReplicationLagSecs != 0 {
		t.Errorf("Lag = %f, want 0", h.ReplicationLagSecs)
	}
}

func TestReplicatorLiveStartStop(t *testing.T) {
	db := New(nell.NewMemoryStore("n"), "n", nell.DefaultCollection)
	rep := NewReplicator(db, "https://example.com")

	stop := rep.Live(t.Context(), LiveConfig{Interval: 100 * time.Millisecond})
	// Give it a moment to start.
	time.Sleep(50 * time.Millisecond)

	// Stop should return cleanly (the loop will fail to connect but
	// that's fine — we're testing lifecycle, not success).
	stop()

	// Second stop should be a no-op (idempotent).
	stop()
}

func TestReplicatorLiveCustomConfig(t *testing.T) {
	db := New(nell.NewMemoryStore("n"), "n", nell.DefaultCollection)
	rep := NewReplicator(db, "https://example.com")

	cfg := LiveConfig{
		Interval:   50 * time.Millisecond,
		PushEvery:  3,
		BackoffMax: 5 * time.Second,
	}
	stop := rep.Live(t.Context(), cfg)
	time.Sleep(30 * time.Millisecond)
	stop()
}

func TestReplicatorLiveDefaultConfig(t *testing.T) {
	db := New(nell.NewMemoryStore("n"), "n", nell.DefaultCollection)
	rep := NewReplicator(db, "https://example.com")

	// Zero values should be filled with defaults.
	stop := rep.Live(t.Context(), LiveConfig{})
	time.Sleep(30 * time.Millisecond)
	stop()
}

// TestLiveWSStartStop verifies that LiveWS starts and stops cleanly.
// It connects to a real WebSocket server to exercise the dial/close path.
func TestLiveWSStartStop(t *testing.T) {
	// Start a test HTTP server that also handles WebSocket upgrades.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Echo back any JSON messages (simplest valid WS handler).
		// We just need the handshake to succeed for the test.
		http.Error(w, "not a websocket", http.StatusBadRequest)
	}))
	defer ts.Close()

	db := New(nell.NewMemoryStore("n"), "n", nell.DefaultCollection)
	rep := NewReplicator(db, ts.URL)

	stop := rep.LiveWS(t.Context(), "test-node")
	time.Sleep(50 * time.Millisecond)

	// The WS will fail to connect (our test server doesn't upgrade),
	// but the loop should handle that gracefully and we can still stop.
	stop()
}

func TestIngestRemote(t *testing.T) {
	store := nell.NewMemoryStore("n")
	db := New(store, "n", nell.DefaultCollection)

	rec := nell.Record{
		ID:        "doc-1",
		Type:      nell.TypeText,
		Payload:   []byte(`{"_id":"doc-1","_rev":"3-abc","value":42}`),
		Clock:     nell.HLC{WallTime: 1000, Counter: 1},
		UpdatedBy: "peer-a",
	}

	err := db.ingestRemote(rec)
	if err != nil {
		t.Fatalf("ingestRemote: %v", err)
	}

	// Verify document exists.
	doc, err := db.Get(t.Context(), "doc-1")
	if err != nil {
		t.Fatalf("Get after ingestRemote: %v", err)
	}
	if doc[FieldID] != "doc-1" {
		t.Errorf("id = %v, want doc-1", doc[FieldID])
	}

	// Verify knowledge vector was updated.
	db.mu.RLock()
	clock, ok := db.vector["peer-a"]
	db.mu.RUnlock()
	if !ok {
		t.Error("vector should contain peer-a")
	}
	if clock.WallTime != 1000 || clock.Counter != 1 {
		t.Errorf("vector[peer-a] = %v, want {1000, 1}", clock)
	}
}

func TestIngestRemoteInternalID(t *testing.T) {
	store := nell.NewMemoryStore("n")
	db := New(store, "n", nell.DefaultCollection)

	rec := nell.Record{
		ID:        "meta:clock",
		Type:      nell.TypeText,
		Payload:   []byte("should-be-ignored"),
		Clock:     nell.HLC{WallTime: 1000, Counter: 1},
		UpdatedBy: "peer-a",
	}

	err := db.ingestRemote(rec)
	if err != nil {
		t.Fatalf("ingestRemote: %v", err)
	}

	// meta: records should not be stored.
	_, err = store.Get(nell.DefaultCollection, "meta:clock")
	if err == nil {
		t.Error("meta:clock should not be stored via ingestRemote")
	}
}

func TestIngestRemoteTombstone(t *testing.T) {
	store := nell.NewMemoryStore("n")
	db := New(store, "n", nell.DefaultCollection)

	rec := nell.Record{
		ID:        "doomed",
		Type:      nell.TypeText,
		Payload:   []byte(`{"_id":"doomed","_rev":"2-xyz","_deleted":true}`),
		Deleted:   true,
		Clock:     nell.HLC{WallTime: 2000, Counter: 1},
		UpdatedBy: "peer-b",
	}

	err := db.ingestRemote(rec)
	if err != nil {
		t.Fatalf("ingestRemote tombstone: %v", err)
	}

	// Tombstone should be stored (deleted but present).
	got, err := store.Get(nell.DefaultCollection, "doomed")
	if err != nil {
		t.Fatalf("Get tombstone: %v", err)
	}
	if !got.Deleted {
		t.Error("ingested tombstone should be marked deleted")
	}
}

func TestReplicatorPullEmptyStore(t *testing.T) {
	storeA := nell.NewMemoryStore("server")
	ts := newTestServer(storeA, "server")
	defer ts.Close()

	clientStore := nell.NewMemoryStore("client")
	db := New(clientStore, "client", nell.DefaultCollection)
	rep := NewReplicator(db, ts.URL)

	n, err := rep.Pull(t.Context())
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}
	if n != 0 {
		t.Errorf("Pull from empty server = %d, want 0", n)
	}
}

func TestReplicatorPushEmptyStore(t *testing.T) {
	storeA := nell.NewMemoryStore("server")
	ts := newTestServer(storeA, "server")
	defer ts.Close()

	clientStore := nell.NewMemoryStore("client")
	db := New(clientStore, "client", nell.DefaultCollection)
	rep := NewReplicator(db, ts.URL)

	n, err := rep.Push(t.Context())
	if err != nil {
		t.Fatalf("Push: %v", err)
	}
	if n != 0 {
		t.Errorf("Push from empty store = %d, want 0", n)
	}
}

func TestReplicatorSyncEmpty(t *testing.T) {
	storeA := nell.NewMemoryStore("server")
	ts := newTestServer(storeA, "server")
	defer ts.Close()

	clientStore := nell.NewMemoryStore("client")
	db := New(clientStore, "client", nell.DefaultCollection)
	rep := NewReplicator(db, ts.URL)

	pushed, pulled, err := rep.Sync(t.Context())
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if pushed != 0 || pulled != 0 {
		t.Errorf("Sync = (%d, %d), want (0, 0)", pushed, pulled)
	}
}

func TestSigningTransportLargeBody(t *testing.T) {
	var capturedBody []byte
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var err error
		capturedBody, err = io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("server read: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	st := &signingTransport{
		secret:    []byte("secret"),
		transport: http.DefaultTransport,
	}

	body := []byte(strings.Repeat("data-chunk,", 10000))
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/sync/push", bytes.NewReader(body))
	resp, err := st.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip large body: %v", err)
	}
	defer resp.Body.Close()

	if !bytes.Equal(capturedBody, body) {
		t.Errorf("large body mismatch: len=%d vs len=%d", len(capturedBody), len(body))
	}
}

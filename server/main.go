// Package server provides the HTTP API, WebSocket fan-out, and peer
// replication for NellDB.  It can be embedded in any Go application
// (standalone mode) or connected to other NellDB instances (mesh mode).
package server

import (
	"encoding/binary"
	"encoding/json"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"sort"
	"sync"

	"github.com/gorilla/websocket"
	"github.com/samcharles93/NellDB"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// MaxBodyBytes is the maximum request body size accepted by sync endpoints.
// Incoming JSON larger than this limit is rejected with 413 Request Entity Too Large.
const MaxBodyBytes = 10 << 20 // 10 MiB

// ── Server ────────────────────────────────────────────────────────────────────

// Server wraps a nell.Store and exposes HTTP sync endpoints.  Start it with
// ListenAndServe or mount its handler into an existing Go HTTP mux.
type Server struct {
	store  nell.Store
	nodeID string

	mu         sync.RWMutex
	peers      map[string]*peerConn // connected WebSocket peers
	kv         nell.KnowledgeVector
	authSecret []byte // optional HMAC secret for sync endpoint auth

	metrics     *Metrics     // optional; nil means metrics are disabled
	meshManager *MeshManager // optional; attached for sync health reporting
}

// New creates a Server backed by the given Store.
func New(store nell.Store, nodeID string) *Server {
	s := &Server{
		store:  store,
		nodeID: nodeID,
		peers:  make(map[string]*peerConn),
		kv:     make(nell.KnowledgeVector),
	}
	s.seedKnowledgeVector()
	return s
}

// SetAuthSecret configures an HMAC shared secret for authenticating sync
// requests.  When set, all /sync/* endpoints (including WebSocket) require
// valid X-Nell-Timestamp and X-Nell-Signature headers.
func (s *Server) SetAuthSecret(secret []byte) {
	s.mu.Lock()
	s.authSecret = secret
	s.mu.Unlock()
}

// seedKnowledgeVector populates the in-memory knowledge vector from the store
// on startup so handleCheck doesn't start from zero.
func (s *Server) seedKnowledgeVector() {
	if kvs, ok := s.store.(interface{ KnowledgeVector() nell.KnowledgeVector }); ok {
		s.mu.Lock()
		maps.Copy(s.kv, kvs.KnowledgeVector())
		s.mu.Unlock()
		return
	}
	// Fallback: scan on startup.
	all, err := s.store.GetChangesSince(nell.HLC{})
	if err != nil {
		return
	}
	s.mu.Lock()
	for _, rec := range all {
		s.kv.Update(rec.UpdatedBy, rec.Clock)
	}
	s.mu.Unlock()
}

// Handler returns an http.Handler for the server's sync API.
func (s *Server) Handler() http.Handler {
	// Top-level mux for health/ready (no auth — these are health probes).
	top := http.NewServeMux()
	top.HandleFunc("/health", s.handleHealth)
	top.HandleFunc("/ready", s.handleReady)

	// Sync mux with all /sync/* routes.
	sync := http.NewServeMux()
	sync.HandleFunc("/sync/pull", s.handlePull)
	sync.HandleFunc("/sync/push", s.handlePush)
	sync.HandleFunc("/sync/check", s.handleCheck)
	sync.HandleFunc("/sync/bin/check", s.handleBinCheck)
	sync.HandleFunc("/sync/bin/push", s.handleBinPush)
	sync.HandleFunc("/sync/ws", s.handleWebSocket)

	// Wrap sync routes with HMAC auth if configured.
	var syncHandler http.Handler = sync
	if len(s.authSecret) > 0 {
		syncHandler = HMACAuth(s.authSecret)(sync)
	}
	top.Handle("/sync/", syncHandler)

	return requireJSON(top)
}

// ... (other handlers)

// handleBinCheck is the high-performance binary version of handleCheck.
func (s *Server) handleBinCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 1. Read Knowledge Vector
	kvBytes, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	kv := make(nell.KnowledgeVector)
	if err := kv.UnmarshalBinary(kvBytes); err != nil {
		http.Error(w, "kv unmarshal: "+err.Error(), http.StatusBadRequest)
		return
	}

	col := r.URL.Query().Get("col")
	if col == "" {
		col = nell.DefaultCollection
	}

	// Use ListAll instead of List so tombstones propagate via anti-entropy.
	all, err := s.store.ListAll(col)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Sort by HLC for monotonic progress
	sort.Slice(all, func(i, j int) bool {
		return all[j].Clock.GreaterThan(all[i].Clock)
	})

	// 2. Stream back missing records in binary
	w.Header().Set("Content-Type", "application/octet-stream")
	w.WriteHeader(http.StatusOK)

	for _, rec := range all {
		seen, ok := kv[rec.UpdatedBy]
		if !ok || rec.Clock.GreaterThan(seen) {
			recBytes, err := rec.MarshalBinary()
			if err != nil {
				continue
			}
			var header [4]byte
			binary.BigEndian.PutUint32(header[:], uint32(len(recBytes)))
			w.Write(header[:])
			w.Write(recBytes)
		}
	}
}

// handleBinPush is the high-performance binary version of handlePush.
func (s *Server) handleBinPush(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	accepted := 0
	total := 0
	var acceptedRecs []nell.Record

	for {
		var header [4]byte
		_, err := io.ReadFull(r.Body, header[:])
		if err == io.EOF {
			break
		}
		if err != nil {
			break
		}

		recLen := binary.BigEndian.Uint32(header[:])
		recBytes := make([]byte, recLen)
		if _, err := io.ReadFull(r.Body, recBytes); err != nil {
			break
		}

		var rec nell.Record
		if err := rec.UnmarshalBinary(recBytes); err != nil {
			continue
		}

		total++
		ok, _, err := s.store.Put(rec)
		if err == nil && ok {
			accepted++
			acceptedRecs = append(acceptedRecs, rec)
		}
		s.recordSeen(rec)
	}

	// Broadcast accepted changes to connected WebSocket peers, same as
	// handlePush does.  The WebSocket frame is JSON-encoded; a future
	// optimisation could send binary frames to binary-native peers.
	if len(acceptedRecs) > 0 {
		s.broadcast(acceptedRecs)
	}

	var resp [4]byte
	binary.BigEndian.PutUint32(resp[:], uint32(accepted))
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Write(resp[:])

	if s.metrics != nil {
		s.metrics.RecordPush(r.Context(), accepted, total)
	}
}

// requireJSON returns middleware that rejects requests without
// Content-Type: application/json on POST endpoints.
func requireJSON(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only enforce on POST endpoints.
		if r.Method == http.MethodPost {
			ct := r.Header.Get("Content-Type")
			// Allow octet-stream for binary endpoints
			if ct == "application/octet-stream" {
				next.ServeHTTP(w, r)
				return
			}
			if ct != "application/json" {
				http.Error(w, "unsupported content type", http.StatusUnsupportedMediaType)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// ListenAndServe starts the server on the given address.
func (s *Server) ListenAndServe(addr string) error {
	slog.Info("NellDB server listening", "node", s.nodeID, "addr", addr)
	return http.ListenAndServe(addr, s.Handler())
}

// SetMetrics attaches a Metrics instance for recording request-level counters.
func (s *Server) SetMetrics(m *Metrics) { s.metrics = m }

// SetMeshManager attaches a MeshManager for sync health reporting.
func (s *Server) SetMeshManager(pm *MeshManager) { s.meshManager = pm }

// ── HTTP Handlers ─────────────────────────────────────────────────────────────

// handlePull streams all records with a clock newer than the client's
// lastKnownClock.
func (s *Server) handlePull(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Since nell.HLC `json:"since"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, MaxBodyBytes)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBodyError(w, err)
		return
	}

	col := r.URL.Query().Get("col")
	if col == "" {
		col = nell.DefaultCollection
	}

	changes, err := s.store.GetChangesSince(req.Since)
	if err != nil {
		logError("handlePull", "store error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Filter by collection
	filtered := make([]nell.Record, 0)
	for _, rec := range changes {
		if rec.Collection == col {
			filtered = append(filtered, rec)
		}
	}
	changes = filtered

	if changes == nil {
		changes = []nell.Record{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"changes": changes,
	})
}

// handlePush accepts a batch of records from a client or peer, applies each
// through LWW conflict resolution, updates the local knowledge vector, and
// broadcasts to connected peers.
func (s *Server) handlePush(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Changes []nell.Record `json:"changes"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, MaxBodyBytes)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBodyError(w, err)
		return
	}
	accepted := 0
	for _, rec := range req.Changes {
		ok, _, err := s.store.Put(rec)
		if err != nil {
			logError("handlePush", "store error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if ok {
			accepted++
		}
		// Update local knowledge vector so anti-entropy knows we've seen
		// this record from this node.
		s.recordSeen(rec)
	}
	// Broadcast to connected peers
	s.broadcast(req.Changes)

	writeJSON(w, http.StatusOK, map[string]any{
		"accepted": accepted,
		"total":    len(req.Changes),
	})

	if s.metrics != nil {
		s.metrics.RecordPush(r.Context(), accepted, len(req.Changes))
	}
}

// handleCheck is the anti-entropy endpoint.  A peer sends its KnowledgeVector
// and receives back all records it is missing.
func (s *Server) handleCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		SenderNodeID string               `json:"sender_node_id"`
		Vector       nell.KnowledgeVector `json:"vector"`
		Limit        int                  `json:"limit"`
		Cursor       string               `json:"cursor"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, MaxBodyBytes)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBodyError(w, err)
		return
	}

	col := r.URL.Query().Get("col")
	if col == "" {
		col = nell.DefaultCollection
	}

	// Find records the sender hasn't seen, with pagination.
	// Use ListAll instead of List so tombstones propagate via anti-entropy.
	all, err := s.store.ListAll(col)
	if err != nil {
		logError("handleCheck", "store error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Sort by HLC so Knowledge Vector progress is monotonic across batches.
	sort.Slice(all, func(i, j int) bool {
		return all[j].Clock.GreaterThan(all[i].Clock)
	})

	limit := clamp(req.Limit, 100, 10000)
	cursor := req.Cursor

	var missing []nell.Record
	pastCursor := cursor == ""
	for _, rec := range all {
		if !pastCursor {
			if rec.ID <= cursor {
				continue
			}
			pastCursor = true
		}
		if len(missing) >= limit {
			break
		}
		seen, ok := req.Vector[rec.UpdatedBy]
		if !ok || rec.Clock.GreaterThan(seen) {
			missing = append(missing, rec)
		}
	}

	// Check if there are more records beyond this page.
	hasMore := false
	if len(missing) >= limit {
		lastID := missing[len(missing)-1].ID
		pastCursor := false
		for _, rec := range all {
			if rec.ID <= lastID {
				continue
			}
			pastCursor = true
			seen, ok := req.Vector[rec.UpdatedBy]
			if !ok || rec.Clock.GreaterThan(seen) {
				hasMore = true
				break
			}
		}
		_ = pastCursor
	}

	var nextCursor string
	if hasMore && len(missing) > 0 {
		nextCursor = missing[len(missing)-1].ID
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"receiver_node_id": s.nodeID,
		"missing_changes":  missing,
		"next_cursor":      nextCursor,
		"has_more":         hasMore,
	})
}

// ── Peer Communication ────────────────────────────────────────────────────────

type peerConn struct {
	nodeID string
	mu     sync.Mutex
	conn   *websocket.Conn
}

// handleWebSocket upgrades the HTTP connection to a WebSocket for real-time
// replication and listens for incoming changes.
func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Error("websocket upgrade failed", "err", err)
		return
	}

	nodeID := r.URL.Query().Get("node_id")
	if nodeID == "" {
		nodeID = "unknown"
	}

	p := &peerConn{
		nodeID: nodeID,
		conn:   conn,
	}

	remoteAddr := r.RemoteAddr
	s.mu.Lock()
	s.peers[remoteAddr] = p
	s.mu.Unlock()

	go func() {
		defer func() {
			s.mu.Lock()
			delete(s.peers, remoteAddr)
			s.mu.Unlock()
			conn.Close()
		}()

		for {
			var req struct {
				Changes []nell.Record `json:"changes"`
			}
			if err := conn.ReadJSON(&req); err != nil {
				break
			}

			accepted := 0
			for _, rec := range req.Changes {
				ok, _, err := s.store.Put(rec)
				if err != nil {
					slog.Error("websocket store put error", "err", err)
					continue
				}
				if ok {
					accepted++
				}
				s.recordSeen(rec)
			}

			if accepted > 0 {
				s.broadcast(req.Changes)
			}
		}
	}()
}

// recordSeen updates the local knowledge vector with a record we've ingested.
// Called from handlePush and the anti-entropy reconciliation loop.
func (s *Server) recordSeen(rec nell.Record) {
	s.mu.Lock()
	s.kv.Update(rec.UpdatedBy, rec.Clock)
	s.mu.Unlock()
}

// broadcast pushes records to all connected WebSocket peers.
func (s *Server) broadcast(changes []nell.Record) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, p := range s.peers {
		go func(p *peerConn) {
			p.mu.Lock()
			defer p.mu.Unlock()
			if err := p.conn.WriteJSON(map[string]any{"changes": changes}); err != nil {
				slog.Error("websocket broadcast failed", "peer", p.nodeID, "err", err)
			} else {
				slog.Info("[broadcast] records", "peer", p.nodeID, "count", len(changes))
			}
		}(p)
	}
}

// ── Health & readiness ────────────────────────────────────────────────────────

// handleHealth is a liveness probe — returns 200 as long as the process is alive.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	resp := map[string]any{
		"status":  "ok",
		"node_id": s.nodeID,
	}
	if s.meshManager != nil {
		resp["sync"] = s.meshManager.Health()
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleReady is a readiness probe — verifies the store is operational.
func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	// A cheap store check: list one record.
	all, err := s.store.List(nell.DefaultCollection)
	if err != nil {
		slog.Error("readiness check failed", "err", err)
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"status": "not ready",
			"error":  "store unavailable",
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":    "ready",
		"node_id":   s.nodeID,
		"doc_count": len(all),
	})
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// writeBodyError maps decode errors from MaxBytesReader to 413, others to 400.
func writeBodyError(w http.ResponseWriter, err error) {
	if err.Error() == "http: request body too large" {
		http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
		return
	}
	http.Error(w, "bad request", http.StatusBadRequest)
}

// logError logs an error with context, keyed by operation name.
func logError(op, msg string, err error) {
	slog.Error(msg, "op", op, "err", err)
}

func clamp(v, lo, hi int) int {
	if v <= 0 {
		return lo
	}
	if v < lo {
		v = lo
	}
	if v > hi {
		v = hi
	}
	return v
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	// Marshal first so we never send a partial body under a 200 OK.
	body, err := json.Marshal(v)
	if err != nil {
		slog.Error("writeJSON marshal failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if _, err := w.Write(body); err != nil {
		slog.Error("writeJSON write failed", "err", err)
	}
}

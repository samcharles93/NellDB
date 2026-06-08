// Package server provides the HTTP API, WebSocket fan-out, and peer
// replication for nell-engine.  It can be embedded in any Go application
// (standalone mode) or connected to other nell-engine instances (mesh mode).
package server

import (
	"encoding/json"
	"log/slog"
	"maps"
	"net/http"
	"sync"

	"github.com/samcharles93/nell-engine"
)

// MaxBodyBytes is the maximum request body size accepted by sync endpoints.
// Incoming JSON larger than this limit is rejected with 413 Request Entity Too Large.
const MaxBodyBytes = 10 << 20 // 10 MiB

// ── Server ────────────────────────────────────────────────────────────────────

// Server wraps a nell.Store and exposes HTTP sync endpoints.  Start it with
// ListenAndServe or mount its handler into an existing Go HTTP mux.
type Server struct {
	store  nell.Store
	nodeID string

	mu    sync.RWMutex
	peers map[string]*peerConn // connected WebSocket peers
	kv    nell.KnowledgeVector

	metrics *Metrics // optional; nil means metrics are disabled
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
	all, err := s.store.List()
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
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/ready", s.handleReady)
	mux.HandleFunc("/sync/pull", s.handlePull)
	mux.HandleFunc("/sync/push", s.handlePush)
	mux.HandleFunc("/sync/check", s.handleCheck)
	return requireJSON(mux)
}

// requireJSON returns middleware that rejects requests without
// Content-Type: application/json on POST endpoints.
func requireJSON(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only enforce on POST endpoints.
		if r.Method == http.MethodPost {
			ct := r.Header.Get("Content-Type")
			if ct != "" && ct != "application/json" {
				http.Error(w, "unsupported content type", http.StatusUnsupportedMediaType)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// ListenAndServe starts the server on the given address.
func (s *Server) ListenAndServe(addr string) error {
	slog.Info("nell-engine server listening", "node", s.nodeID, "addr", addr)
	return http.ListenAndServe(addr, s.Handler())
}

// SetMetrics attaches a Metrics instance for recording request-level counters.
func (s *Server) SetMetrics(m *Metrics) { s.metrics = m }

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
	changes, err := s.store.GetChangesSince(req.Since)
	if err != nil {
		logError("handlePull", "store error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
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

	// Find records the sender hasn't seen, with pagination.
	all, err := s.store.List()
	if err != nil {
		logError("handleCheck", "store error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

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
	// TODO: WebSocket connection for real-time push
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
		_ = p // TODO: WebSocket send
		slog.Info("[broadcast] records", "peer", p.nodeID, "count", len(changes))
	}
}

// ── Health & readiness ────────────────────────────────────────────────────────

// handleHealth is a liveness probe — returns 200 as long as the process is alive.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "node_id": s.nodeID})
}

// handleReady is a readiness probe — verifies the store is operational.
func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	// A cheap store check: list one record.
	all, err := s.store.List()
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

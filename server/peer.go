package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"math/rand"
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/samcharles93/NellDB"
)

// ── PeerManager interface ────────────────────────────────────────────────────

// PeerManager is the contract for peer discovery, mutation broadcast, and
// anti-entropy reconciliation.  The MeshManager struct below is the default
// implementation; WebSocket broadcast support can be added later without
// changing the interface.
type PeerManager interface {
	BroadcastMutation(rec nell.Record)
	ReconcileWithPeer(peerURL string) error
	GetLocalKnowledgeVector() nell.KnowledgeVector
}

// ── MeshManager ─────────────────────────────────────────────────────────────

// MeshManager periodically reconciles with known peers via /sync/check.
// It implements the PeerManager interface.
type MeshManager struct {
	srv        *Server
	peers      []string
	mu         sync.RWMutex
	client     *http.Client
	ticker     *time.Ticker
	stopCh     chan struct{}
	interval   time.Duration
	authSecret []byte // HMAC shared secret for signing peer requests
}

// NewMeshManager creates a mesh manager that calls /sync/check on a random
// peer every interval.  peers may be empty — call AddPeer later.
// authSecret, when non-empty, is used to HMAC-sign all peer requests.
func NewMeshManager(srv *Server, peers []string, interval time.Duration, authSecret []byte) *MeshManager {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	return &MeshManager{
		srv:        srv,
		peers:      append([]string{}, peers...),
		client:     &http.Client{Timeout: 10 * time.Second},
		stopCh:     make(chan struct{}),
		interval:   interval,
		authSecret: authSecret,
	}
}

// AddPeer registers a peer URL for periodic reconciliation.
func (pm *MeshManager) AddPeer(url string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	if slices.Contains(pm.peers, url) {
		return
	}
	pm.peers = append(pm.peers, url)
}

// RemovePeer unregisters a peer URL.
func (pm *MeshManager) RemovePeer(url string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	for i, p := range pm.peers {
		if p == url {
			pm.peers[i] = pm.peers[len(pm.peers)-1]
			pm.peers = pm.peers[:len(pm.peers)-1]
			return
		}
	}
}

// Peers returns a snapshot of the current peer list.
func (pm *MeshManager) Peers() []string {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	out := make([]string, len(pm.peers))
	copy(out, pm.peers)
	return out
}

// Start begins the periodic anti-entropy loop in a background goroutine.
// Idempotent — calling Start multiple times is a no-op.
func (pm *MeshManager) Start() {
	pm.mu.Lock()
	if pm.ticker != nil {
		pm.mu.Unlock()
		return
	}
	pm.ticker = time.NewTicker(pm.interval)
	tickerC := pm.ticker.C // capture before releasing lock
	pm.mu.Unlock()

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

	go func() {
		slog.Info("[mesh] anti-entropy loop started", "interval", pm.interval, "peers", pm.Peers())
		for {
			select {
			case <-pm.stopCh:
				slog.Info("[mesh] anti-entropy loop stopped")
				return
			case <-tickerC:
				pm.mu.RLock()
				peers := make([]string, len(pm.peers))
				copy(peers, pm.peers)
				pm.mu.RUnlock()

				if len(peers) == 0 {
					continue
				}
				peer := peers[rng.Intn(len(peers))]
				if err := pm.reconcileOne(peer); err != nil {
					slog.Error("[mesh] reconcile failed", "peer", peer, "err", err)
				}
			}
		}
	}()
}

// Stop halts the periodic loop.  Safe to call on a nil or stopped manager.
func (pm *MeshManager) Stop() {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	if pm.ticker != nil {
		pm.ticker.Stop()
	}
	select {
	case pm.stopCh <- struct{}{}:
	default:
	}
}

// ── PeerManager interface implementation ─────────────────────────────────

// BroadcastMutation stubs the WebSocket push path.
func (pm *MeshManager) BroadcastMutation(rec nell.Record) {
	_ = rec // TODO: WebSocket fan-out
}

// ReconcileWithPeer performs a one-shot /sync/check → ingest cycle with peerURL.
func (pm *MeshManager) ReconcileWithPeer(peerURL string) error {
	return pm.reconcileOne(peerURL)
}

// GetLocalKnowledgeVector returns a copy of the server's knowledge vector.
func (pm *MeshManager) GetLocalKnowledgeVector() nell.KnowledgeVector {
	return pm.srv.knowledgeVector()
}

// ── Reconciliation logic ─────────────────────────────────────────────────

func (pm *MeshManager) reconcileOne(peerURL string) error {
	kv := pm.srv.knowledgeVector()

	body, err := json.Marshal(map[string]any{
		"sender_node_id": pm.srv.nodeID,
		"vector":         kv,
	})
	if err != nil {
		return fmt.Errorf("marshal check request: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), pm.client.Timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		joinURL(peerURL, "/sync/check"), bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// HMAC-sign the request if configured.
	if len(pm.authSecret) > 0 {
		ts := time.Now().Unix()
		req.Header.Set("X-Nell-Timestamp", fmt.Sprintf("%d", ts))
		req.Header.Set("X-Nell-Signature", SignBody(pm.authSecret, ts, body))
	}

	resp, err := pm.client.Do(req)
	if err != nil {
		return fmt.Errorf("post /sync/check: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("check %s: %s", peerURL, string(raw))
	}

	var out struct {
		Missing    []nell.Record `json:"missing_changes"`
		NextCursor string        `json:"next_cursor"`
		HasMore    bool          `json:"has_more"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return fmt.Errorf("decode check response: %w", err)
	}

	// Ingest this page.
	ingested := 0
	for _, rec := range out.Missing {
		if _, _, err := pm.srv.store.Put(rec); err != nil {
			return fmt.Errorf("ingest %q from %s: %w", rec.ID, peerURL, err)
		}
		pm.srv.recordSeen(rec)
		ingested++
	}

	// Follow pagination cursor until all missing records are collected.
	cursor := out.NextCursor
	for out.HasMore && cursor != "" {
		kv2 := pm.srv.knowledgeVector()
		reqBody := map[string]any{
			"sender_node_id": pm.srv.nodeID,
			"vector":         kv2,
			"cursor":         cursor,
		}
		body2, err := json.Marshal(reqBody)
		if err != nil {
			return fmt.Errorf("marshal paginated check: %w", err)
		}

		ctx2, cancel2 := context.WithTimeout(context.Background(), pm.client.Timeout)
		req2, err := http.NewRequestWithContext(ctx2, http.MethodPost,
			joinURL(peerURL, "/sync/check"), bytes.NewReader(body2))
		if err != nil {
			cancel2()
			return fmt.Errorf("build paginated request: %w", err)
		}
		req2.Header.Set("Content-Type", "application/json")
		if len(pm.authSecret) > 0 {
			ts := time.Now().Unix()
			req2.Header.Set("X-Nell-Timestamp", fmt.Sprintf("%d", ts))
			req2.Header.Set("X-Nell-Signature", SignBody(pm.authSecret, ts, body2))
		}

		resp2, err := pm.client.Do(req2)
		cancel2()
		if err != nil {
			return fmt.Errorf("paginated check post: %w", err)
		}
		if resp2.StatusCode != http.StatusOK {
			raw, _ := io.ReadAll(io.LimitReader(resp2.Body, 4096))
			_ = resp2.Body.Close()
			return fmt.Errorf("paginated check %s: %s", peerURL, string(raw))
		}

		var out2 struct {
			Missing    []nell.Record `json:"missing_changes"`
			NextCursor string        `json:"next_cursor"`
			HasMore    bool          `json:"has_more"`
		}
		if err := json.NewDecoder(resp2.Body).Decode(&out2); err != nil {
			_ = resp2.Body.Close()
			return fmt.Errorf("decode paginated check: %w", err)
		}
		_ = resp2.Body.Close()

		for _, rec := range out2.Missing {
			if _, _, err := pm.srv.store.Put(rec); err != nil {
				return fmt.Errorf("ingest %q from %s: %w", rec.ID, peerURL, err)
			}
			pm.srv.recordSeen(rec)
			ingested++
		}
		out.HasMore = out2.HasMore
		cursor = out2.NextCursor
	}

	if ingested > 0 {
		slog.Info("[peer-mgr] got missing records", "count", ingested, "peer", peerURL)
	}
	return nil
}

// ── helpers ───────────────────────────────────────────────────────────────

// knowledgeVector returns a copy of the in-memory knowledge vector.
// Seeded from the store on startup and kept current by recordSeen.
// Always O(number of peer nodes), never O(number of records).
func (s *Server) knowledgeVector() nell.KnowledgeVector {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(nell.KnowledgeVector, len(s.kv))
	maps.Copy(out, s.kv)
	return out
}

func joinURL(base, path string) string {
	// Simple concatenation — assumes base doesn't have a trailing slash
	// and path starts with /.
	return base + path
}

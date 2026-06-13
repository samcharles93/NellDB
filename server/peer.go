package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/http"
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

// MeshManager periodically reconciles with known peers via /sync/check
// and tracks peer health with a heartbeat loop.
// It implements the PeerManager interface.
type MeshManager struct {
	srv        *Server
	peers      map[string]*TrackedPeer // URL → peer state
	mu         sync.RWMutex
	client     *http.Client
	ticker     *time.Ticker
	stopCh     chan struct{}
	interval   time.Duration
	authSecret []byte // HMAC shared secret for signing peer requests
}

// NewMeshManager creates a mesh manager that calls /sync/check on all active
// peers every interval and tracks peer health via heartbeats.
// peers may be empty — call AddPeer later.
// authSecret, when non-empty, is used to HMAC-sign all peer requests.
func NewMeshManager(srv *Server, peers []string, interval time.Duration, authSecret []byte) *MeshManager {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	pm := &MeshManager{
		srv:        srv,
		peers:      make(map[string]*TrackedPeer),
		client:     &http.Client{Timeout: 10 * time.Second},
		stopCh:     make(chan struct{}),
		interval:   interval,
		authSecret: authSecret,
	}
	for _, url := range peers {
		pm.peers[url] = newTrackedPeer(url)
	}
	return pm
}

// AddPeer registers a peer URL for periodic reconciliation and heartbeats.
func (pm *MeshManager) AddPeer(url string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	if _, exists := pm.peers[url]; exists {
		return
	}
	pm.peers[url] = newTrackedPeer(url)
}

// RemovePeer unregisters a peer URL.
func (pm *MeshManager) RemovePeer(url string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	delete(pm.peers, url)
}

// Peers returns a snapshot of the current active peer URLs.
func (pm *MeshManager) Peers() []string {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	var out []string
	for _, p := range pm.peers {
		if p.isActive() {
			out = append(out, p.URL)
		}
	}
	return out
}

// PeersAll returns all tracked peers regardless of state (for debugging).
func (pm *MeshManager) PeersAll() []*TrackedPeer {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	out := make([]*TrackedPeer, 0, len(pm.peers))
	for _, p := range pm.peers {
		out = append(out, p)
	}
	return out
}

// Start begins the periodic anti-entropy loop and heartbeat goroutine.
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

	// Anti-entropy loop: reconciles with all active peers each tick.
	go func() {
		slog.Info("[mesh] anti-entropy loop started", "interval", pm.interval, "peers", pm.Peers())
		// Semaphore limits concurrent reconciles to avoid overwhelming
		// the network or the local store.
		const maxConcurrent = 4
		for {
			select {
			case <-pm.stopCh:
				slog.Info("[mesh] anti-entropy loop stopped")
				return
			case <-tickerC:
				active := pm.Peers()
				if len(active) == 0 {
					continue
				}
				sem := make(chan struct{}, maxConcurrent)
				for _, peer := range active {
					sem <- struct{}{}
					go func(url string) {
						defer func() { <-sem }()
						if err := pm.reconcileOne(url); err != nil {
							slog.Error("[mesh] reconcile failed", "peer", url, "err", err)
						}
					}(peer)
				}
			}
		}
	}()

	// Heartbeat loop: pings every peer to track health state.
	go func() {
		hbInterval := DefaultHeartbeatInterval
		slog.Info("[mesh] heartbeat loop started", "interval", hbInterval)
		hbTicker := time.NewTicker(hbInterval)
		defer hbTicker.Stop()
		for {
			select {
			case <-pm.stopCh:
				slog.Info("[mesh] heartbeat loop stopped")
				return
			case <-hbTicker.C:
				pm.heartbeatAll()
			}
		}
	}()
}

// Stop halts all background goroutines (anti-entropy loop and heartbeat).
// Safe to call on a nil or stopped manager.  Idempotent.
func (pm *MeshManager) Stop() {
	pm.mu.Lock()
	if pm.ticker != nil {
		pm.ticker.Stop()
	}
	pm.mu.Unlock()

	// Close stopCh to signal all goroutines — both anti-entropy and heartbeat.
	select {
	case <-pm.stopCh:
		// Already closed.
	default:
		close(pm.stopCh)
	}
}

// ── PeerManager interface implementation ─────────────────────────────────

// BroadcastMutation pushes a local mutation to all connected WebSocket peers.
func (pm *MeshManager) BroadcastMutation(rec nell.Record) {
	pm.srv.broadcast([]nell.Record{rec})
}

// ReconcileWithPeer performs a one-shot /sync/check → ingest cycle with
// peerURL.  The peer is added to the tracking set if not already present.
func (pm *MeshManager) ReconcileWithPeer(peerURL string) error {
	pm.mu.Lock()
	if _, exists := pm.peers[peerURL]; !exists {
		pm.peers[peerURL] = newTrackedPeer(peerURL)
	}
	pm.mu.Unlock()
	return pm.reconcileOne(peerURL)
}

// GetLocalKnowledgeVector returns a copy of the server's knowledge vector.
func (pm *MeshManager) GetLocalKnowledgeVector() nell.KnowledgeVector {
	return pm.srv.knowledgeVector()
}

// ── Heartbeat logic ─────────────────────────────────────────────────────

// heartbeatAll sends a HEAD /health to every tracked peer and updates state.
func (pm *MeshManager) heartbeatAll() {
	pm.mu.RLock()
	urls := make([]string, 0, len(pm.peers))
	for url := range pm.peers {
		urls = append(urls, url)
	}
	pm.mu.RUnlock()

	for _, url := range urls {
		pm.mu.RLock()
		p, ok := pm.peers[url]
		pm.mu.RUnlock()
		if !ok {
			continue
		}

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		req, err := http.NewRequestWithContext(ctx, http.MethodHead,
			joinURL(url, "/health"), nil)
		if err != nil {
			cancel()
			p.recordMiss(DefaultMaxMissedPings)
			continue
		}
		resp, err := pm.client.Do(req)
		cancel()
		if err != nil || resp.StatusCode != http.StatusOK {
			if resp != nil {
				resp.Body.Close()
			}
			p.recordMiss(DefaultMaxMissedPings)
			continue
		}
		resp.Body.Close()
		p.recordPing()
	}
}

// ── Reconciliation logic ─────────────────────────────────────────────────

func (pm *MeshManager) reconcileOne(peerURL string) error {
	// Skip peers that are not active (degraded or dead).
	pm.mu.RLock()
	p, ok := pm.peers[peerURL]
	pm.mu.RUnlock()
	if ok && !p.isActive() {
		return fmt.Errorf("peer %s is %s, skipping reconcile", peerURL, p.getState())
	}

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

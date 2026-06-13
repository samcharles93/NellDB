package server

import (
	"sync"
	"time"
)

// PeerState represents the health state of a tracked peer.
type PeerState string

const (
	StateActive   PeerState = "active"
	StateDegraded PeerState = "degraded"
	StateDead     PeerState = "dead"
)

const (
	// DefaultHeartbeatInterval is how often we ping peers.
	DefaultHeartbeatInterval = 10 * time.Second
	// DefaultMaxMissedPings before a peer is considered dead.
	DefaultMaxMissedPings = 3
)

// TrackedPeer wraps a peer URL with health state tracked by the heartbeat loop.
type TrackedPeer struct {
	URL         string
	NodeID      string
	State       PeerState
	LastSeen    time.Time
	MissedPings int

	mu sync.RWMutex
}

// newTrackedPeer initializes a peer in the active state.
func newTrackedPeer(url string) *TrackedPeer {
	return &TrackedPeer{
		URL:      url,
		State:    StateActive,
		LastSeen: time.Now(),
	}
}

// getState returns the current peer state (thread-safe).
func (p *TrackedPeer) getState() PeerState {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.State
}

// isActive returns true if the peer is in the active state.
func (p *TrackedPeer) isActive() bool {
	return p.getState() == StateActive
}

// recordPing updates state after a successful ping.
func (p *TrackedPeer) recordPing() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.LastSeen = time.Now()
	p.MissedPings = 0
	p.State = StateActive
}

// recordMiss updates state after a failed ping. Returns the new state.
func (p *TrackedPeer) recordMiss(maxMissed int) PeerState {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.MissedPings++
	switch {
	case p.MissedPings >= maxMissed:
		p.State = StateDead
	case p.MissedPings >= 1:
		p.State = StateDegraded
	}
	return p.State
}

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
	url         string
	nodeID      string
	state       PeerState
	lastSeen    time.Time
	missedPings int

	mu sync.RWMutex
}

func (p *TrackedPeer) URL() string        { p.mu.RLock(); defer p.mu.RUnlock(); return p.url }
func (p *TrackedPeer) State() PeerState   { p.mu.RLock(); defer p.mu.RUnlock(); return p.state }
func (p *TrackedPeer) LastSeen() time.Time { p.mu.RLock(); defer p.mu.RUnlock(); return p.lastSeen }
func (p *TrackedPeer) NodeID() string     { p.mu.RLock(); defer p.mu.RUnlock(); return p.nodeID }

// newTrackedPeer initializes a peer in the active state.
func newTrackedPeer(url string) *TrackedPeer {
	return &TrackedPeer{
		url:      url,
		state:    StateActive,
		lastSeen: time.Now(),
	}
}

// getState returns the current peer state (thread-safe).
func (p *TrackedPeer) getState() PeerState {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.state
}

// isActive returns true if the peer is in the active state.
func (p *TrackedPeer) isActive() bool {
	return p.getState() == StateActive
}

// recordPing updates state after a successful ping.
func (p *TrackedPeer) recordPing() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.lastSeen = time.Now()
	p.missedPings = 0
	p.state = StateActive
}

// recordMiss updates state after a failed ping. Returns the new state.
func (p *TrackedPeer) recordMiss(maxMissed int) PeerState {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.missedPings++
	switch {
	case p.missedPings >= maxMissed:
		p.state = StateDead
	case p.missedPings >= 1:
		p.state = StateDegraded
	}
	return p.state
}

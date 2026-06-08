package nell

import (
	"errors"
	"fmt"
	"time"
)

// ErrRecordNotFound is returned by Store.Get when the requested ID does not
// exist or has been tombstoned.  Use errors.Is to check.
var ErrRecordNotFound = errors.New("nell: record not found")

// DataType discriminates the kind of payload in a Record.
type DataType string

const (
	TypeText   DataType = "text"
	TypeVector DataType = "vector"
	TypeImage  DataType = "image"
)

// ── Hybrid Logical Clock ──────────────────────────────────────────────────────

// HLC combines physical wall time with a logical counter to provide causally-
// consistent ordering across disconnected nodes without a central coordinator.
type HLC struct {
	WallTime int64 `json:"wall_time"` // Unix milliseconds
	Counter  int32 `json:"counter"`   // per-millisecond monotonic tick
}

// NewHLC returns a fresh HLC snapped to the current system time.
func NewHLC() HLC {
	return HLC{WallTime: time.Now().UnixMilli(), Counter: 0}
}

// Tick advances the clock for a local write.
// If the physical clock has advanced, the counter resets; otherwise it increments.
func (h *HLC) Tick() HLC {
	now := time.Now().UnixMilli()
	if now > h.WallTime {
		h.WallTime = now
		h.Counter = 0
	} else {
		h.Counter++
	}
	return *h
}

// Update merges a received peer clock into the local clock.  Must be called
// before applying any incoming mutation so that subsequent local ticks stay
// causally ahead of all known events.
func (h *HLC) Update(peer HLC) {
	if peer.WallTime > h.WallTime {
		h.WallTime = peer.WallTime
		h.Counter = peer.Counter + 1
		return
	}
	if peer.WallTime == h.WallTime && peer.Counter >= h.Counter {
		h.Counter = peer.Counter + 1
	}
}

// GreaterThan is the total order predicate: a > b ⇔ a happened-after or is
// concurrent-but-deterministically-greater.
func (a HLC) GreaterThan(b HLC) bool {
	if a.WallTime != b.WallTime {
		return a.WallTime > b.WallTime
	}
	return a.Counter > b.Counter
}

// Equal reports whether two clocks represent the exact same logical instant.
func (a HLC) Equal(b HLC) bool {
	return a.WallTime == b.WallTime && a.Counter == b.Counter
}

func (a HLC) String() string {
	return fmt.Sprintf("%d:%d", a.WallTime, a.Counter)
}

// ── Record ────────────────────────────────────────────────────────────────────

// Record is the universal document type carried by NellDB.  Text and image
// payloads live in Payload; vector embeddings live in Vector.  The Clock field
// provides causal ordering; UpdatedBy identifies the originating node.
type Record struct {
	ID        string    `json:"id"`
	Type      DataType  `json:"type"`
	Payload   []byte    `json:"payload,omitempty"`
	Vector    []float32 `json:"vector,omitempty"`
	Clock     HLC       `json:"clock"`
	UpdatedBy string    `json:"updated_by"`
	Deleted   bool      `json:"deleted"`
}

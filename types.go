package nell

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"time"
)

// ErrRecordNotFound is returned by Store.Get when the requested ID does not
// exist or has been tombstoned.  Use errors.Is to check.
var ErrRecordNotFound = errors.New("nell: record not found")

// DataType discriminates the kind of payload in a Record.
type DataType = string

const (
	TypeText   DataType = "text"
	TypeVector DataType = "vector"
	TypeImage  DataType = "image"
)

const DefaultCollection = "default"

// ── Hybrid Logical Clock ──────────────────────────────────────────────────────

// HLC combines physical wall time with a logical counter to provide causally-
// consistent ordering across disconnected nodes without a central coordinator.
type HLC struct {
	WallTime int64 `json:"wall_time"` // Unix milliseconds
	Counter  int32 `json:"counter"`   // per-millisecond monotonic tick
}

const HLCSize = 12 // 8 bytes wall_time + 4 bytes counter

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

// EncodeBinary writes the HLC to a 12-byte buffer.
func (h HLC) EncodeBinary(b []byte) {
	binary.BigEndian.PutUint64(b[0:8], uint64(h.WallTime))
	binary.BigEndian.PutUint32(b[8:12], uint32(h.Counter))
}

// DecodeBinary reads the HLC from a 12-byte buffer.
func (h *HLC) DecodeBinary(b []byte) {
	h.WallTime = int64(binary.BigEndian.Uint64(b[0:8]))
	h.Counter = int32(binary.BigEndian.Uint32(b[8:12]))
}

// ── Record ────────────────────────────────────────────────────────────────────

// Record is the universal document type carried by NellDB.  Text and image
// payloads live in Payload; vector embeddings live in Vector.  The Clock field
// provides causal ordering; UpdatedBy identifies the originating node.
type Record struct {
	Collection string    `json:"collection"`
	ID         string    `json:"id"`
	Type       string    `json:"type"`
	Payload    []byte    `json:"payload,omitempty"`
	Vector     []float32 `json:"vector,omitempty"`
	Clock      HLC       `json:"clock"`
	UpdatedBy  string    `json:"updated_by"`
	Deleted    bool      `json:"deleted"`
}

// Binary encoding layout:
// [1 byte: version]
// [1 byte: deleted (0/1)]
// [12 bytes: HLC clock]
// [2 bytes: collection len]
// [N bytes: collection string]
// [2 bytes: id len]
// [N bytes: id string]
// [2 bytes: type len]
// [N bytes: type string]
// [2 bytes: updatedBy len]
// [N bytes: updatedBy string]
// [4 bytes: vector float32 count]
// [N*4 bytes: vector data]
// [4 bytes: payload len]
// [N bytes: payload data]

func (r Record) MarshalBinary() ([]byte, error) {
	size := 1 + 1 + HLCSize + 2 + len(r.Collection) + 2 + len(r.ID) + 2 + len(r.Type) + 2 + len(r.UpdatedBy) + 4 + len(r.Vector)*4 + 4 + len(r.Payload)
	b := make([]byte, size)

	b[0] = 1 // version
	if r.Deleted {
		b[1] = 1
	} else {
		b[1] = 0
	}
	r.Clock.EncodeBinary(b[2:14])

	off := 14
	binary.BigEndian.PutUint16(b[off:], uint16(len(r.Collection)))
	off += 2
	copy(b[off:], r.Collection)
	off += len(r.Collection)

	binary.BigEndian.PutUint16(b[off:], uint16(len(r.ID)))
	off += 2
	copy(b[off:], r.ID)
	off += len(r.ID)

	binary.BigEndian.PutUint16(b[off:], uint16(len(r.Type)))
	off += 2
	copy(b[off:], r.Type)
	off += len(r.Type)

	binary.BigEndian.PutUint16(b[off:], uint16(len(r.UpdatedBy)))
	off += 2
	copy(b[off:], r.UpdatedBy)
	off += len(r.UpdatedBy)

	binary.BigEndian.PutUint32(b[off:], uint32(len(r.Vector)))
	off += 4
	for _, v := range r.Vector {
		bits := math.Float32bits(v)
		binary.BigEndian.PutUint32(b[off:], bits)
		off += 4
	}

	binary.BigEndian.PutUint32(b[off:], uint32(len(r.Payload)))
	off += 4
	copy(b[off:], r.Payload)

	return b, nil
}

func (r *Record) UnmarshalBinary(b []byte) error {
	if len(b) < 14 {
		return errors.New("nell: binary record too short")
	}
	// version := b[0]
	r.Deleted = b[1] == 1
	r.Clock.DecodeBinary(b[2:14])

	off := 14

	// Collection
	if len(b) < off+2 {
		return errors.New("nell: truncated collection len")
	}
	cL := int(binary.BigEndian.Uint16(b[off:]))
	off += 2
	if len(b) < off+cL {
		return errors.New("nell: truncated collection string")
	}
	r.Collection = string(b[off : off+cL])
	off += cL

	// ID
	if len(b) < off+2 {
		return errors.New("nell: truncated id len")
	}
	iL := int(binary.BigEndian.Uint16(b[off:]))
	off += 2
	if len(b) < off+iL {
		return errors.New("nell: truncated id string")
	}
	r.ID = string(b[off : off+iL])
	off += iL

	// Type
	if len(b) < off+2 {
		return errors.New("nell: truncated type len")
	}
	tL := int(binary.BigEndian.Uint16(b[off:]))
	off += 2
	if len(b) < off+tL {
		return errors.New("nell: truncated type string")
	}
	r.Type = string(b[off : off+tL])
	off += tL

	// UpdatedBy
	if len(b) < off+2 {
		return errors.New("nell: truncated updatedBy len")
	}
	uL := int(binary.BigEndian.Uint16(b[off:]))
	off += 2
	if len(b) < off+uL {
		return errors.New("nell: truncated updatedBy string")
	}
	r.UpdatedBy = string(b[off : off+uL])
	off += uL

	// Vector
	if len(b) < off+4 {
		return errors.New("nell: truncated vector count")
	}
	vC := int(binary.BigEndian.Uint32(b[off:]))
	off += 4
	if len(b) < off+vC*4 {
		return errors.New("nell: truncated vector data")
	}
	r.Vector = make([]float32, vC)
	for i := range vC {
		bits := binary.BigEndian.Uint32(b[off:])
		r.Vector[i] = math.Float32frombits(bits)
		off += 4
	}

	// Payload
	if len(b) < off+4 {
		return errors.New("nell: truncated payload len")
	}
	pL := int(binary.BigEndian.Uint32(b[off:]))
	off += 4
	if len(b) < off+pL {
		return errors.New("nell: truncated payload data")
	}
	r.Payload = make([]byte, pL)
	copy(r.Payload, b[off:off+pL])

	return nil
}

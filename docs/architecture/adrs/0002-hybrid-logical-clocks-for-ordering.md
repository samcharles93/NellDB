# ADR 0002: Hybrid Logical Clocks for Causality

- **Status:** Accepted
- **Date:** 2026-06-07

## Context

Clients can be offline for extended periods (days to weeks). When they reconnect, their locally-timed writes must be ordered against writes that happened on other nodes during the offline period. 

Physical wall clocks are unreliable:
- NTP sync is not guaranteed on mobile devices or in Electron apps.
- Clock drift across distributed servers can exceed seconds or minutes.
- Two events on different machines with the same wall-clock timestamp are ambiguous.

Monotonic clocks (e.g. `time.Now().UnixNano()`) are per-process and meaningless across machines.
Version vectors or vector clocks solve this but carry per-replica state that grows with the cluster.

A Hybrid Logical Clock (HLC) combines physical time with a logical counter: it provides causal consistency equivalent to a vector clock with only ~16 bytes of state, and it converges to physical time when nodes communicate.

## Decision

Implement HLC as a two-field struct:

```go
type HLC struct {
    WallTime int64 `json:"wall_time"` // Unix milliseconds
    Counter  int32 `json:"counter"`   // per-millisecond tick
}
```

The HLC guarantees:
- If event B **happened-after** event A on any node, `B.Clock.GreaterThan(A.Clock) == true`.
- If `A.Clock == B.Clock`, the events are concurrent (or identical).
- On reconnection, a node's physical clock is compared to the peer's clock — if the peer's WallTime is ahead, the local clock jumps forward, preserving the "happened-before" relation across the network.

Comparison is implemented as:

```go
func (a HLC) GreaterThan(b HLC) bool {
    if a.WallTime != b.WallTime {
        return a.WallTime > b.WallTime
    }
    return a.Counter > b.Counter
}
```

Every record carries its HLC at the time of mutation. The HLC is updated atomically with every write:

1. Read current wall time.
2. If wall time > local `WallTime`, set `WallTime = wall time`, reset `Counter = 0`.
3. If wall time <= local `WallTime`, increment `Counter`.
4. If a peer sends a clock higher than local `WallTime`, jump forward.

## Consequences

### Positive

- Strict causal ordering without a central coordinator.
- Tiny metadata footprint: 12 bytes per record plus the node ID string.
- Converges to physical time naturally during communication.
- Deterministic — the same sequence of events always produces the same clock ordering.

### Negative

- Requires maintaining a stateful clock component on each node (wall time + counter).
- Clock jumps on reconnection can cause large gaps in WallTime if the local clock was far behind — this is correct but can be surprising.
- Counter may overflow if >2B events occur in a single millisecond (not a practical concern).

---

**Implementation:** `core/types.go` (HLC struct), maintained in `core/store.go` during Put operations.

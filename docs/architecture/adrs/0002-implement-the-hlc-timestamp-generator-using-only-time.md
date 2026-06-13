# ADR 0002: Implement the HLC timestamp generator using only `time.

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** client
- **Proposed by:** gopher_fundamentalist-05
- **Net votes:** +2

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Implement the HLC timestamp generator using only `time.Now().UnixNano()` and `sync/atomic` for the logical counter — a single `uint64` combining physical time (ms since epoch) in upper 48 bits and logical counter in lower 16 bits; `CompareAndSwap` loop guarantees monotonicity across goroutines without mutexes, and `encoding/binary.BigEndian.PutUint64` serializes it for the 24-byte record header.

## Consequences

*To be determined as the architecture is implemented.*

---

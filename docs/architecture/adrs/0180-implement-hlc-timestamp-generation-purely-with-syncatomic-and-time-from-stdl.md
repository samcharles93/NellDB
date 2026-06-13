# ADR 0180: Implement HLC timestamp generation purely with sync/atomic and time from stdl...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** core
- **Proposed by:** gopher_fundamentalist-10
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Implement HLC timestamp generation purely with sync/atomic and time from stdlib: pack wall-time (millis since epoch, 48 bits) and logical counter (16 bits) into a single uint64; use atomic.CompareAndSwapUint64 in a tight loop for thread-safe increments that work identically in WASM and native. On recv, advance local HLC via atomic.Max then increment; no external clock libraries, no mutexes, zero allocations — just two stdlib packages and raw bit ops.

## Consequences

*To be determined as the architecture is implemented.*

---

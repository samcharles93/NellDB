# ADR 0110: Architect the entire engine as a single owner-goroutine per client that holds...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** architecture
- **Proposed by:** gopher_fundamentalist-05
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Architect the entire engine as a single owner-goroutine per client that holds all collection state as local variables (flat []byte logs + parallel []uint64 key-hash indexes) and processes serialized commands from a standard library channel; network I/O runs in two stdlib goroutines (net/http + gorilla/websocket via syscall/js for WASM) that only speak JSON-over-WebSocket frames carrying length-prefixed CBOR records (encoding/binary), while conflict resolution, compaction, and HLC advancement are pure functions executed exclusively by the owner — zero mutexes, zero background workers, zero external deps, ~3KB WASM.

## Consequences

*To be determined as the architecture is implemented.*

---

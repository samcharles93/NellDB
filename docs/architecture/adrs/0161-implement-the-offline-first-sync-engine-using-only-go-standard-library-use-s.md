# ADR 0161: Implement the offline-first sync engine using only Go standard library: use s...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** core
- **Proposed by:** gopher_fundamentalist-10
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Implement the offline-first sync engine using only Go standard library: use sync.Map for concurrent document storage, channels for HLC-ordered operation queues, and encoding/binary for LWW conflict resolution with deterministic byte comparison — no external deps, compiles cleanly to WASM via TinyGo.

## Consequences

*To be determined as the architecture is implemented.*

---

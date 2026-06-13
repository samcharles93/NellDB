# ADR 0187: Implement the sync protocol as a pure Go pipeline using unbuffered channels f...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** core
- **Proposed by:** gopher_fundamentalist-10
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Implement the sync protocol as a pure Go pipeline using unbuffered channels for backpressure: a single goroutine per remote peer reads length-prefixed frames via io.Reader, validates HLC ordering with sync/atomic sequence counters, and fans mutations into a shared apply-channel guarded by a sync.Mutex-protected ring buffer (container/ring) — zero external deps, zero reflection, and the channel semantics naturally enforce causal ordering without vector-clock metadata bloat.

## Consequences

*To be determined as the architecture is implemented.*

---

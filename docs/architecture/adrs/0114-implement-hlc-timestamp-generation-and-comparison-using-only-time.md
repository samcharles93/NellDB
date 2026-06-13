# ADR 0114: Implement HLC timestamp generation and comparison using only time.

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** architecture
- **Proposed by:** gopher_fundamentalist-10
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Implement HLC timestamp generation and comparison using only time.Time, sync/atomic, and math/big from the standard library — no external clock libraries. Represent the sync frontier as a map[string]*hlc.Timestamp protected by sync.RWMutex, and drive the replication loop with a single goroutine per remote peer using stdlib channels for backpressure and context.Context for cancellation.

## Consequences

*To be determined as the architecture is implemented.*

---

# ADR 0060: Build the sync protocol handler using only net/http, context, and sync primit...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** server
- **Proposed by:** gopher_fundamentalist-05
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Build the sync protocol handler using only net/http, context, and sync primitives from the standard library; implement HLC timestamp generation and LWW conflict resolution as pure functions operating on byte slices, with goroutine-per-connection state machines driven by channels for backpressure and cancellation.

## Consequences

*To be determined as the architecture is implemented.*

---

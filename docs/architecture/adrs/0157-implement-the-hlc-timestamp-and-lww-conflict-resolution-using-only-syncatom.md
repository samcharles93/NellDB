# ADR 0157: Implement the HLC timestamp and LWW conflict resolution using only `sync/atom...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** core
- **Proposed by:** gopher_fundamentalist-10
- **Net votes:** +2

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Implement the HLC timestamp and LWW conflict resolution using only `sync/atomic` for monotonic counters, `time` package for wall-clock with `sync.Mutex` guards, and `encoding/binary` for deterministic wire encoding — no external clock libraries or custom serialization frameworks. The sync pipeline uses a fixed worker pool of `goroutines` fed by buffered `channels` with `sync.WaitGroup` for graceful shutdown, and `net/http` with `http.Transport` tuned for intermittent connectivity handles the home-server sync with exponential backoff via `time.Ticker`.

## Consequences

*To be determined as the architecture is implemented.*

---

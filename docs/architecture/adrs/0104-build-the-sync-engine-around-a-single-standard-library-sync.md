# ADR 0104: Build the sync engine around a single standard-library sync.

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** architecture
- **Proposed by:** gopher_fundamentalist-05
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Build the sync engine around a single standard-library sync.Mutex-protected in-memory B-tree (using container/btree or a flat slice with binary search) for each data type, with a background goroutine per client persisting mutations to a WAL via os.File + bufio.Writer; use stdlib sync/atomic for HLC counters and channels for backpressure between the offline queue and the sync loop — zero external deps, pure Go primitives.

## Consequences

*To be determined as the architecture is implemented.*

---

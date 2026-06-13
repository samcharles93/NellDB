# ADR 0062: Implement the sync ingestion pipeline as a single stdlib-only Go routine per ...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** server
- **Proposed by:** gopher_fundamentalist-10
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Implement the sync ingestion pipeline as a single stdlib-only Go routine per collection: net/http for the wire protocol, encoding/binary for HLC timestamp parsing, sync.Mutex for the in-memory LWW map, and os.File with O_APPEND|O_SYNC for the append-only log — no external deps, no framework, just the standard library doing what it was designed for.

## Consequences

*To be determined as the architecture is implemented.*

---

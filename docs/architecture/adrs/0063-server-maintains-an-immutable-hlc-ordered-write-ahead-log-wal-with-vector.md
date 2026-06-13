# ADR 0063: Server maintains an immutable, HLC-ordered write-ahead log (WAL) with vector-...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** server
- **Proposed by:** dist_hardliner-02
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Server maintains an immutable, HLC-ordered write-ahead log (WAL) with vector-clock causality metadata for every mutation; on restart, the WAL is replayed to reconstruct exact state without trusting any client-reported progress, and all sync responses include the server's current HLC timestamp so clients can never silently diverge.

## Consequences

*To be determined as the architecture is implemented.*

---

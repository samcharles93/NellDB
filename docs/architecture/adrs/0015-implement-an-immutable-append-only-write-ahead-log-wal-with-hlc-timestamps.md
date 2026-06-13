# ADR 0015: Implement an immutable, append-only write-ahead log (WAL) with HLC timestamps...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** client
- **Proposed by:** dist_hardliner-07
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Implement an immutable, append-only write-ahead log (WAL) with HLC timestamps persisted to IndexedDB/OPFS, where each entry carries a vector clock digest for causal dependency tracking — this guarantees replayable offline mutations for 7+ days and enables deterministic conflict resolution during sync without trusting server clocks.

## Consequences

*To be determined as the architecture is implemented.*

---

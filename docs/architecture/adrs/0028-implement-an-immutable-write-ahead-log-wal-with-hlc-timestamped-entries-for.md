# ADR 0028: Implement an immutable write-ahead log (WAL) with HLC-timestamped entries for...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** client
- **Proposed by:** dist_hardliner-02
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Implement an immutable write-ahead log (WAL) with HLC-timestamped entries for all local mutations, using a segmented append-only file format with CRC32C checksums per entry; this enables deterministic replay, crash recovery, and causally-correct sync after week-long offline periods without relying on wall-clock accuracy.

## Consequences

*To be determined as the architecture is implemented.*

---

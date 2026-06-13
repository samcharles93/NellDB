# ADR 0162: All data (text, vectors, images) shall be stored in a single contiguous byte ...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** core
- **Proposed by:** wasm_purist-06
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

All data (text, vectors, images) shall be stored in a single contiguous byte arena with fixed-width headers encoding only type, length, and HLC timestamp—no per-record pointers, no secondary indexes, no metadata bloat. Reads and writes operate via direct offset arithmetic on the arena; sync scans the arena linearly for dirty flags. Compaction is a single memmove.

## Consequences

*To be determined as the architecture is implemented.*

---

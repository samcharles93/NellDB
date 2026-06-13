# ADR 0103: Replace map-based indexes with a single flat byte buffer holding fixed-width ...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** architecture
- **Proposed by:** wasm_purist-06
- **Net votes:** +2

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Replace map-based indexes with a single flat byte buffer holding fixed-width 24-byte records (8B HLC + 8B offset + 8B key hash) kept sorted by key hash — binary search for O(log n) lookups with zero allocation, compaction via in-place tombstone sweep during sync write batches, no background goroutines, no mutexes, pure byte-slice manipulation compiling to <5KB WASM.

## Consequences

*To be determined as the architecture is implemented.*

---

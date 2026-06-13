# ADR 0025: Embed a SIMD-friendly HNSW-lite graph directly into the WAL's overflow region...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** client
- **Proposed by:** embedding_zealot-03
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Embed a SIMD-friendly HNSW-lite graph directly into the WAL's overflow region: each vector record (32-dim int8) reserves 4 bytes for a 16-neighbor adjacency list (uint16 offsets) in the fixed 64-byte slot, enabling branchless top-k search via linear scan over memory-mapped pages with AVX2/NEON dot-product kernels — no separate index files, no heap allocations, and the graph rebuilds incrementally during the existing compaction pass (ID-0293) by re-linking only mutated keys' neighborhoods.

## Consequences

*To be determined as the architecture is implemented.*

---

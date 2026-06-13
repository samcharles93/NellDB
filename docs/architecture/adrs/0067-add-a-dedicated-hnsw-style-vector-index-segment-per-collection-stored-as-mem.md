# ADR 0067: Add a dedicated HNSW-style vector index segment per collection, stored as mem...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** server
- **Proposed by:** embedding_zealot-08
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Add a dedicated HNSW-style vector index segment per collection, stored as memory-mapped flat files with quantized (int8) centroids and SIMD-accelerated distance kernels — embeddings are appended to the log like payloads but also routed to the index segment for sub-millisecond ANN search without loading full vectors into heap.

## Consequences

*To be determined as the architecture is implemented.*

---

# ADR 0105: Embed a compact HNSW-lite index directly into each collection's WAL segment f...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** architecture
- **Proposed by:** embedding_zealot-08
- **Net votes:** +3

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Embed a compact HNSW-lite index directly into each collection's WAL segment footer, storing quantized 8-bit vectors in a SIMD-friendly SoA layout so that offline clients can run k-NN search over local embeddings without loading full payloads; the index rebuilds incrementally during sync replay using only the new WAL entries, keeping memory overhead under 2 MB per 10k vectors and enabling linear-scan fallback via AVX2/NEON dot-product kernels when the graph is stale.

## Consequences

*To be determined as the architecture is implemented.*

---

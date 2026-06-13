# ADR 0189: Implement a unified embedding quantizer that stores vectors as SIMD-aligned i...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** core
- **Proposed by:** embedding_zealot-08
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Implement a unified embedding quantizer that stores vectors as SIMD-aligned int8 blocks with per-block dynamic scale factors, enabling exact cosine similarity via dot-product + scale correction without dequantization; pair this with a fused AVX2/NEON kernel that computes 128 int8 distances per cycle and feeds directly into the HNSW graph traversal, eliminating heap allocations and cache misses during offline ANN search.

## Consequences

*To be determined as the architecture is implemented.*

---

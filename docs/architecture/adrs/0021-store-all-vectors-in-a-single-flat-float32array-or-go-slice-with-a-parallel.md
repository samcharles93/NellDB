# ADR 0021: Store all vectors in a single flat Float32Array (or Go slice) with a parallel...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** client
- **Proposed by:** embedding_zealot-03
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Store all vectors in a single flat Float32Array (or Go slice) with a parallel uint32 index mapping vector_id → offset, enabling cache-friendly SIMD dot-product scans across the entire corpus without pointer chasing; pair this with a tiny 4-bit quantization sidecar for progressive refinement during ANN search on WASM where AVX2 is unavailable.

## Consequences

*To be determined as the architecture is implemented.*

---

# ADR 0016: Store embeddings in a single columnar float32 array (interleaved dim-major) w...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** client
- **Proposed by:** embedding_zealot-08
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Store embeddings in a single columnar float32 array (interleaved dim-major) with a parallel uint64 HLC timestamp array; run on-device ANN search via SIMD-optimized flat linear scan (AVX2/NEON/WASM SIMD128) capped at 10k vectors, deferring exact k-NN to server sync — no HNSW, no quantization, just contiguous memory and cache-friendly stride loops.

## Consequences

*To be determined as the architecture is implemented.*

---

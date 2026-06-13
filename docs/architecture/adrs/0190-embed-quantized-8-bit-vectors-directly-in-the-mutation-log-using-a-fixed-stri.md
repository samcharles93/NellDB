# ADR 0190: Embed quantized 8-bit vectors directly in the mutation log using a fixed stri...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** core
- **Proposed by:** embedding_zealot-08
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Embed quantized 8-bit vectors directly in the mutation log using a fixed stride layout (dimension count × 1 byte) with a precomputed SIMD lookup table for asymmetric distance computation; this enables linear brute-force KNN search over the entire log at 2-3 GB/s per core on WASM SIMD without any secondary index structures or heap allocations.

## Consequences

*To be determined as the architecture is implemented.*

---

# ADR 0033: Implement a local quantized embedding index (int8/uint8 HNSW or IVF-Flat) wit...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** client
- **Proposed by:** embedding_zealot-03
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Implement a local quantized embedding index (int8/uint8 HNSW or IVF-Flat) with SIMD-accelerated distance kernels (AVX2/NEON/WASM SIMD128) for offline semantic search and deduplication; store vectors in a memory-mapped columnar array (SoA layout) alongside the WAL to enable zero-copy similarity scans and incremental index updates during week-long offline sessions without full rebuilds.

## Consequences

*To be determined as the architecture is implemented.*

---

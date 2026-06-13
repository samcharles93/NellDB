# ADR 0066: Add a product quantization (PQ) layer atop the HNSW graph: split each 4-byte ...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** server
- **Proposed by:** embedding_zealot-08
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Add a product quantization (PQ) layer atop the HNSW graph: split each 4-byte quantized vector into M sub-vectors, precompute 256-entry SIMD-friendly distance lookup tables per sub-quantizer, and answer ANN queries via a single linear pass with horizontal SIMD reductions — eliminating heap allocations, keeping the entire index mmap-resident, and enabling exact reranking on the original float16 vectors stored in the columnar SOA layout.

## Consequences

*To be determined as the architecture is implemented.*

---

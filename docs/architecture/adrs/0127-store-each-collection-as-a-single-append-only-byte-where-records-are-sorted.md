# ADR 0127: Store each collection as a single append-only []byte where records are sorted...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** architecture
- **Proposed by:** embedding_zealot-03
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Store each collection as a single append-only []byte where records are sorted by a 64-bit Morton code derived from the document's 256-dim embedding (quantized to 64 bits via SIMD-accelerated random projection), enabling both exact key lookups via binary search AND approximate k-NN scans via linear range reads — no separate vector index, no HNSW graph, just cache-friendly spatial locality; sync transmits only the Morton-sorted delta, and conflicts are resolved by comparing embedding cosine similarity (SIMD dot-product) instead of timestamps when keys collide, keeping the entire engine under 3KB WASM with zero external deps.

## Consequences

*To be determined as the architecture is implemented.*

---

# ADR 0058: Server stores vectors in a columnar quantized block format: 4-bit product qua...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** server
- **Proposed by:** embedding_zealot-08
- **Net votes:** +2

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Server stores vectors in a columnar quantized block format: 4-bit product quantization codes + 8-bit L2 norms per subvector, packed into 64-byte cache-line-aligned blocks. A separate lightweight HNSW graph over PQ centroids (not raw vectors) enables graph search; linear SIMD scan over quantized blocks serves as fallback and brute-force rerank. Ingest pipeline normalizes incoming FP32 vectors, trains a PQ codebook on the first 10k vectors, then appends only compressed blocks — full-precision vectors never hit disk. Search endpoint accepts optional `precision=linear|graph|hybrid` param; hybrid runs graph to 100 candidates then SIMD-reranks over quantized blocks, returning top-k with reconstructed scores.

## Consequences

*To be determined as the architecture is implemented.*

---

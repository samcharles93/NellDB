# ADR 0179: Add a quantized vector column (uint8/int8) alongside the raw float32 payload ...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** core
- **Proposed by:** embedding_zealot-08
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Add a quantized vector column (uint8/int8) alongside the raw float32 payload using a per-origin product quantization codebook trained incrementally via k-means on incoming embeddings; store codes in a SoA layout with 64-byte aligned blocks for AVX2/NEON popcount distance kernels, enabling 8-16x storage reduction and sub-millisecond ANN search during offline reconciliation without heap allocations. During sync, prioritize transmission of vector neighborhoods with highest quantization distortion first, using the codebook residuals as a semantic diff heuristic to minimize bandwidth for perceptually similar content.

## Consequences

*To be determined as the architecture is implemented.*

---

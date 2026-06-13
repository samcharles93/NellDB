# ADR 0013: Implement on-device embedding generation using a quantized MobileBERT/ONNX mo...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** client
- **Proposed by:** embedding_zealot-08
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Implement on-device embedding generation using a quantized MobileBERT/ONNX model with int8 weights, storing vectors in a columnar SoA layout aligned to 64-byte cache lines for branchless SIMD distance computation during local ANN search — embeddings become queryable immediately without server round-trip.

## Consequences

*To be determined as the architecture is implemented.*

---

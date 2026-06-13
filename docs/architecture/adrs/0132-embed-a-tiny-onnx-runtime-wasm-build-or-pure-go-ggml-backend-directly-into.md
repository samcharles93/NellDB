# ADR 0132: Embed a tiny ONNX Runtime WASM build (or pure-Go GGML backend) directly into ...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** architecture
- **Proposed by:** embedding_zealot-03
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Embed a tiny ONNX Runtime WASM build (or pure-Go GGML backend) directly into the client to generate 256-dim embeddings locally from text/image patches — store vectors in a columnar SoA buffer (float32[4][N]) aligned to 64-byte cache lines so AVX2/NEON dot products and 8-bit PQ quantization run without transposition; during sync, transmit only residual PQ codes + 4-bit error deltas, reconstructing full precision server-side via a shared codebook, keeping offline search latency sub-millisecond and sync payload under 32 bytes/vector.

## Consequences

*To be determined as the architecture is implemented.*

---

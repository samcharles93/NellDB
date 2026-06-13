# ADR 0168: Store vector payloads in a columnar, SIMD-friendly flatbuffer layout (aligned...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** core
- **Proposed by:** embedding_zealot-03
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Store vector payloads in a columnar, SIMD-friendly flatbuffer layout (aligned 32-byte chunks) so ANN search runs as tight linear scans with AVX2/NEON dot products — no heap allocations, no pointer chasing. Embeddings are quantized to int8 at ingest with per-block dynamic scaling, keeping the hot index under 15 MB for 1M vectors and enabling offline k-NN at 200k QPS on a phone CPU. Write-path appends quantized blocks to the WAL alongside the HLC timestamp, making vector sync just another ordered log segment.

## Consequences

*To be determined as the architecture is implemented.*

---

# ADR 0059: Integrate a lightweight HNSW vector index into the sync handler to enable sem...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** server
- **Proposed by:** embedding_zealot-03
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Integrate a lightweight HNSW vector index into the sync handler to enable semantic deduplication of incoming payloads before LWW resolution; store embeddings in a memory-mapped spatial array layout (float32, row-major, 64-byte aligned) for SIMD-friendly brute-force fallback and zero-copy WASM sharing with clients.

## Consequences

*To be determined as the architecture is implemented.*

---

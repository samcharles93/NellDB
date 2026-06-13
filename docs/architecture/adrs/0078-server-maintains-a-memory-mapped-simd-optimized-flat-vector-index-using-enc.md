# ADR 0078: Server maintains a memory-mapped, SIMD-optimized flat vector index (using enc...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** server
- **Proposed by:** embedding_zealot-08
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Server maintains a memory-mapped, SIMD-optimized flat vector index (using encoding/binary + unsafe pointer arithmetic) that incrementally builds a navigable small-world graph over document embeddings during ingest; a /search endpoint exposes k-NN queries over the HNSW-like graph without loading full payloads, adding ~10KB WASM using only stdlib math/bits and sync/atomic for lock-free graph traversal.

## Consequences

*To be determined as the architecture is implemented.*

---

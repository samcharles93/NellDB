# ADR 0024: Integrate a tiny quantized embedding model (e.

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** client
- **Proposed by:** embedding_zealot-03
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Integrate a tiny quantized embedding model (e.g., 32-dim int8 from distilled BERT/CLIP) directly into the client's write path, generating vectors on-device for text/image payloads using SIMD-optimized inference kernels — this enables immediate semantic search and embedding-aware conflict resolution during offline operation without any server round-trip, and the fixed 32-byte vector slots align perfectly with the WAL's 64-byte record structure from ID-0306.

## Consequences

*To be determined as the architecture is implemented.*

---

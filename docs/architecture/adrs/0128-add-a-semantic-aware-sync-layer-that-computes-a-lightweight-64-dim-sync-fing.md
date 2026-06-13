# ADR 0128: Add a semantic-aware sync layer that computes a lightweight 64-dim "sync fing...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** architecture
- **Proposed by:** embedding_zealot-03
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Add a semantic-aware sync layer that computes a lightweight 64-dim "sync fingerprint" (via a tiny distillation head on the local 256-dim embeddings) for every document; during sync, peers exchange fingerprint Bloom filters first to detect semantic near-duplicates and divergent branches before transmitting full vectors — this lets the engine skip redundant payload transfers, flag semantic conflicts (cosine < 0.85) for CRDT-style merge instead of LWW, and prioritize sync bandwidth for genuinely novel content, all using the same ONNX/GGML runtime from ID-0272 with zero additional model downloads.

## Consequences

*To be determined as the architecture is implemented.*

---

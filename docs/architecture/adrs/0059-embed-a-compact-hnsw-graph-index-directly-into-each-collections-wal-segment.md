# ADR 0059: Embed a compact HNSW graph index directly into each collection's WAL segment,...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** server
- **Proposed by:** embedding_zealot-03
- **Net votes:** +2

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Embed a compact HNSW graph index directly into each collection's WAL segment, storing 4-byte quantized vectors (uint8 or int8) alongside payloads so that similarity search runs as a cache-friendly linear scan with SIMD dot products — no separate index rebuild, no heap allocations, and the graph structure survives crash recovery intact because it's just more WAL records.

## Consequences

*To be determined as the architecture is implemented.*

---

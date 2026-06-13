# ADR 0172: Store all vector payloads in a single contiguous columnar array (float32[dim]...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** core
- **Proposed by:** embedding_zealot-08
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Store all vector payloads in a single contiguous columnar array (float32[dim][N]) with a parallel HLC-timestamp bitset, enabling SIMD-accelerated linear similarity search and incremental ANN index rebuilds during idle cycles without blocking the sync loop.

## Consequences

*To be determined as the architecture is implemented.*

---

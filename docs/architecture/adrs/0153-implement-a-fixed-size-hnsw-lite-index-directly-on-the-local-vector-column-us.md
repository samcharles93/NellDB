# ADR 0153: Implement a fixed-size HNSW-lite index directly on the local vector column us...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** core
- **Proposed by:** embedding_zealot-08
- **Net votes:** +2

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Implement a fixed-size HNSW-lite index directly on the local vector column using a compact SIMD-friendly memory layout (SoA with 256-bit aligned blocks) so nearest-neighbor search runs entirely in-process without heap allocations; store the graph as a single contiguous uint32 array with per-node neighbor offsets to enable fast linear scans during offline sync reconciliation.

## Consequences

*To be determined as the architecture is implemented.*

---

# ADR 0151: All client-side state must be represented as flat, append-only byte buffers w...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** core
- **Proposed by:** wasm_purist-01
- **Net votes:** +2

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

All client-side state must be represented as flat, append-only byte buffers with fixed-offset indexing — no hash maps, no dynamic allocations, no reflection-based serialization; mutations are applied via in-place byte patching using precomputed field offsets, keeping the WASM binary under 50KB and heap usage deterministic.

## Consequences

*To be determined as the architecture is implemented.*

---

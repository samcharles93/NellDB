# ADR 0178: Implement a unified SIMD distance kernel registry with runtime CPU feature de...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** core
- **Proposed by:** embedding_zealot-08
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Implement a unified SIMD distance kernel registry with runtime CPU feature detection (AVX2, NEON, WASM-SIMD128) that operates directly on the SoA vector column layout; expose a single `search(query_vec, k, filter_fn)` API where `filter_fn` is a compiled bytecode predicate over metadata columns, enabling hybrid vector+attribute search without heap allocations or branch mispredictions during offline reconciliation.

## Consequences

*To be determined as the architecture is implemented.*

---

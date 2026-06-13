# ADR 0125: Encode all persistent frames (text ops, vector deltas, image patches) as fixe...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** architecture
- **Proposed by:** wasm_purist-01
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Encode all persistent frames (text ops, vector deltas, image patches) as fixed-width binary structs with zero padding, using a single contiguous byte slice per WAL segment — no varints, no length prefixes, no reflection-based serialization. Decode via direct unsafe.Pointer casts to typed views, eliminating parse overhead and allocation entirely.

## Consequences

*To be determined as the architecture is implemented.*

---

# ADR 0170: Replace schema registry validation with a single fixed-width binary header (m...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** core
- **Proposed by:** wasm_purist-01
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Replace schema registry validation with a single fixed-width binary header (magic bytes + type tag + HLC timestamp + payload length) — all mutations are raw byte slices appended to the WAL with zero parsing overhead; validation becomes a client-side lint, not a runtime gate.

## Consequences

*To be determined as the architecture is implemented.*

---

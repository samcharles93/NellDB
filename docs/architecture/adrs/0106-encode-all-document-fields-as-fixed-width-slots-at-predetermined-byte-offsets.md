# ADR 0106: Encode all document fields as fixed-width slots at predetermined byte offsets...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** architecture
- **Proposed by:** wasm_purist-01
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Encode all document fields as fixed-width slots at predetermined byte offsets in the single buffer, eliminating all per-field metadata, type tags, and dynamic allocations; use a compile-time schema registry to map field names to (offset, length, encoding) tuples so the runtime performs zero-copy reads/writes via unsafe pointer arithmetic.

## Consequences

*To be determined as the architecture is implemented.*

---

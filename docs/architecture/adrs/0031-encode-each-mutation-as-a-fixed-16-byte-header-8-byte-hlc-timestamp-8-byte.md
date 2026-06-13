# ADR 0031: Encode each mutation as a fixed 16-byte header (8-byte HLC timestamp + 8-byte...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** client
- **Proposed by:** wasm_purist-01
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Encode each mutation as a fixed 16-byte header (8-byte HLC timestamp + 8-byte payload length) followed by raw payload bytes in a single append-only file; use the file offset itself as the sync checkpoint cursor — zero metadata, zero secondary structures, pure sequential I/O.

## Consequences

*To be determined as the architecture is implemented.*

---

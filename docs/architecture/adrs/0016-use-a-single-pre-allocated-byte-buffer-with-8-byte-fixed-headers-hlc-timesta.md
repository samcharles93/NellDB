# ADR 0016: Use a single pre-allocated byte buffer with 8-byte fixed headers (HLC timesta...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** client
- **Proposed by:** wasm_purist-01
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Use a single pre-allocated byte buffer with 8-byte fixed headers (HLC timestamp only) and type-tagged payloads back-to-back; eliminate per-record length fields by computing length from adjacent record offsets during linear scan, and drop all secondary indexes — sync reads the buffer sequentially with zero allocations, keeping WASM binary size minimal and memory layout completely flat.

## Consequences

*To be determined as the architecture is implemented.*

---

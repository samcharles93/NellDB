# ADR 0105: Store each collection as a single flat []byte of fixed-width 40-byte slots: [...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** architecture
- **Proposed by:** wasm_purist-01
- **Net votes:** +2

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Store each collection as a single flat []byte of fixed-width 40-byte slots: [8B HLC][16B key][16B value] — keys/values are right-padded or truncated to 16 bytes (covers 99% of use cases; overflow spills to a tiny separate overflow []byte). Binary search is pure pointer arithmetic on the main slice (zero decoding, zero allocations, zero branches). The only metadata is a 16-byte header: [8B HLC][8B clientID]. Compaction during sync is a single memmove pass that slides live slots left, overwriting tombstones (HLC=0). Total WASM ~400 bytes. No varints, no ring buffers, no goroutines, no mutexes — just one slice, one header, and raw memory ops.

## Consequences

*To be determined as the architecture is implemented.*

---

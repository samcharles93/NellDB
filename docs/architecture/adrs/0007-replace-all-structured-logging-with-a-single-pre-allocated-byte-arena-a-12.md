# ADR 0007: Replace all structured logging with a single pre-allocated []byte arena: a 12...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** client
- **Proposed by:** wasm_purist-01
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Replace all structured logging with a single pre-allocated []byte arena: a 128KB ring buffer where each mutation is a fixed 32-byte header (8B HLC, 8B key-hash, 8B payload-offset, 8B payload-length) followed by inline payload; the sync cursor is a single atomic uint64 file offset. Parsing uses unsafe.Add/Pointer arithmetic over the raw byte slice — zero allocations, zero maps, zero serialization, <5KB WASM binary.

## Consequences

*To be determined as the architecture is implemented.*

---

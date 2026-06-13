# ADR 0177: Replace the mutation log with a fixed-width circular buffer in WASM linear me...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** core
- **Proposed by:** wasm_purist-06
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Replace the mutation log with a fixed-width circular buffer in WASM linear memory: each slot is exactly 256 bytes (64-bit HLC | 16-bit origin | 190-byte payload | 6-byte reserved), eliminating varints, length prefixes, and per-record checksums entirely. Persistence is a single memcpy of the entire buffer to OPFS/IndexedDB as a raw ArrayBuffer — no serialization, no file format, no replay logic. Sync reduces to exchanging (write_ptr, generation) tuples and memcmp-ing the differing slice; conflicts resolve by last-writer-wins on HLC with zero metadata overhead, keeping the WASM binary under 15KB.

## Consequences

*To be determined as the architecture is implemented.*

---

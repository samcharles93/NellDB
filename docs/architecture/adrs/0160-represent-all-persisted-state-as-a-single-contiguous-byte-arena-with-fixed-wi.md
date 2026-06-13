# ADR 0160: Represent all persisted state as a single contiguous byte arena with fixed-wi...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** core
- **Proposed by:** wasm_purist-06
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Represent all persisted state as a single contiguous byte arena with fixed-width slots — no pointers, no interfaces, no reflection. Encode HLC timestamps, vector quanta, and text lengths as raw little-endian integers at known offsets; reads are pure byte-slice arithmetic with zero allocations. The entire hot path compiles to <50 KB WASM by banning string formatting, map access, and any type that escapes to the heap.

## Consequences

*To be determined as the architecture is implemented.*

---

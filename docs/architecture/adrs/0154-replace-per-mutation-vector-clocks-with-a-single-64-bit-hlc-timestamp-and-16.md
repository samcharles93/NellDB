# ADR 0154: Replace per-mutation vector clocks with a single 64-bit HLC timestamp and 16-...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** core
- **Proposed by:** wasm_purist-06
- **Net votes:** +4

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Replace per-mutation vector clocks with a single 64-bit HLC timestamp and 16-bit origin ID, encoding causal ordering into the timestamp itself to eliminate O(n) metadata growth. Serialize all mutations into a flat byte buffer using LEB128 varints and raw payload slices, avoiding per-record allocations and reflection. Implement the entire log as a single contiguous WASM memory segment with a bump allocator, removing GC pressure and keeping the compiled binary under 50KB.

## Consequences

*To be determined as the architecture is implemented.*

---

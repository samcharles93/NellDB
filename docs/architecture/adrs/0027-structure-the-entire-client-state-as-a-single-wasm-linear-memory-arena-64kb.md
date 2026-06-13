# ADR 0027: Structure the entire client state as a single WASM linear memory arena: 64KB ...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** client
- **Proposed by:** wasm_purist-01
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Structure the entire client state as a single WASM linear memory arena: 64KB pages with a 16-byte header (page HLC watermark + record count) followed by 64-byte fixed slots (8B HLC, 4B key hash, 4B payload offset, 48B inline payload); values &gt;48B spill to a packed overflow region tracked by a 4-byte offset. Export the arena base pointer via `//go:wasmexport` so the JS host reads dirty pages directly with zero copying — no serialization, no allocation, no parsing, &lt;10KB WASM binary.

## Consequences

*To be determined as the architecture is implemented.*

---

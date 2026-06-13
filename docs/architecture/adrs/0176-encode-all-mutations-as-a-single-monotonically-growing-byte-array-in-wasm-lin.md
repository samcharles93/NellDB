# ADR 0176: Encode all mutations as a single monotonically-growing byte array in WASM lin...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** core
- **Proposed by:** wasm_purist-06
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Encode all mutations as a single monotonically-growing byte array in WASM linear memory where each record is prefixed by exactly one uint64 packing HLC (48b), origin (16b), and payload length (up to 64KB); sync reduces to exchanging a (base_offset, byte_length) tuple and a single memcpy from sender's memory into receiver's at that offset — no circular buffer, no fixed slots, no generation counters, no log structure whatsoever. Conflict resolution is a direct byte-wise comparison of the two uint64 headers at the same offset: higher HLC wins, payload is already in place. The entire sync protocol fits in 3 KB of WASM code.

## Consequences

*To be determined as the architecture is implemented.*

---

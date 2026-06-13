# ADR 0183: Eliminate all per-mutation metadata and secondary indexes: store mutations as...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** core
- **Proposed by:** wasm_purist-06
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Eliminate all per-mutation metadata and secondary indexes: store mutations as a single flat byte stream [LEB128 HLC | LEB128 origin | raw payload] with a trailing 4-byte xxHash32 of the entire stream for sync integrity — no causal fingerprints, no vector sketches, no Merkle roots, no conflict audit records. During sync, peers exchange only the trailing hash and stream length; divergence is resolved by binary-searching the stream for the first differing byte, then truncating and replaying the suffix. This keeps the WASM binary under 30KB (xxHash32 is ~1KB vs BLAKE3's ~15KB) and reduces per-mutation overhead to exactly 2 varints + payload with zero allocations or indexing structures.

## Consequences

*To be determined as the architecture is implemented.*

---

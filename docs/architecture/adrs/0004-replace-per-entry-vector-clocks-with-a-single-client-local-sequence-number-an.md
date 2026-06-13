# ADR 0004: Replace per-entry vector clocks with a single client-local sequence number an...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** client
- **Proposed by:** wasm_purist-06
- **Net votes:** +2

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Replace per-entry vector clocks with a single client-local sequence number and a 12-byte HLC timestamp; encode mutations as raw CBOR byte slices in a flat append-only file — no secondary indexes, no in-memory trees, just sequential reads for replay and a 64-bit cursor for sync checkpointing.

## Consequences

*To be determined as the architecture is implemented.*

---

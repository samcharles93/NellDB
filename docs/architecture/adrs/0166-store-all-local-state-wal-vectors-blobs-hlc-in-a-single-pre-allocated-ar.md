# ADR 0166: Store all local state (WAL, vectors, blobs, HLC) in a single pre-allocated Ar...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** core
- **Proposed by:** wasm_purist-01
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Store all local state (WAL, vectors, blobs, HLC) in a single pre-allocated ArrayBuffer with typed view slices — no malloc, no GC pressure, no serialization; mutations are direct byte writes at computed offsets, sync reads the same buffer raw for delta encoding, and compaction is a single memcpy sliding valid regions to the front.

## Consequences

*To be determined as the architecture is implemented.*

---

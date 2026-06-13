# ADR 0110: Replace the B-tree with a single append-only flat buffer per data type ([]byt...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** architecture
- **Proposed by:** wasm_purist-06
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Replace the B-tree with a single append-only flat buffer per data type ([]byte) using fixed-width record headers for O(1) append and binary search on sorted offsets; persist by memory-mapping the buffer to disk via WASM's File System Access API, eliminating WAL overhead and keeping the entire index as a contiguous byte slice under 64KB for typical offline-week workloads.

## Consequences

*To be determined as the architecture is implemented.*

---

# ADR 0104: Eliminate all separate index structures: store every collection as a single a...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** architecture
- **Proposed by:** wasm_purist-01
- **Net votes:** +2

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Eliminate all separate index structures: store every collection as a single append-only byte slice where records are packed as [HLC:8][keyLen:2][key][valLen:4][val] in strict key-sorted order; reads binary-search the slice directly by decoding key boundaries on-the-fly (zero allocation, zero metadata), compaction rewrites the entire slice in-place during sync using a two-pointer sweep that overwrites tombstones — no background goroutines, no mutexes, no WAL segments, no footers, just one []byte per collection compiling to <2KB WASM.

## Consequences

*To be determined as the architecture is implemented.*

---

# ADR 0126: Store each collection as a single append-only []byte of pure [varint key_len]...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** architecture
- **Proposed by:** wasm_purist-01
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Store each collection as a single append-only []byte of pure [varint key_len][key][varint val_len][val] records in strict key-sorted order — zero per-record metadata, zero timestamps, zero indexes. Reads binary-search by decoding varints on-the-fly during key comparison (zero allocations). The only HLC state is a single uint64 counter + 8-byte clientID kept in a 16-byte sync header; pending writes accumulate in a tiny 64-byte ring buffer of (key_offset, val_len, HLC) tuples for the outbound sync stream. Compaction is one memmove pass during sync that overwrites tombstones and rewrites the sync header atomically. Total WASM footprint ~800 bytes.

## Consequences

*To be determined as the architecture is implemented.*

---

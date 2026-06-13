# ADR 0188: The write-ahead log shall be a single append-only file per collection, segmen...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** core
- **Proposed by:** gopher_fundamentalist-05
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

The write-ahead log shall be a single append-only file per collection, segmented by time windows, with each entry encoded as a self-describing binary frame (HLC timestamp, vector clock, payload length, payload) — no external serialization library, just encoding/binary and a fixed header struct. In-memory indexing uses a stdlib sync.Map from document ID to a slice of byte offsets into the current segment file, enabling O(1) lookups without btree or LSM dependencies. Compaction rewrites live entries to a new segment file using io.CopyBuffer and atomic rename; readers never block writers because segments are immutable once sealed.

## Consequences

*To be determined as the architecture is implemented.*

---

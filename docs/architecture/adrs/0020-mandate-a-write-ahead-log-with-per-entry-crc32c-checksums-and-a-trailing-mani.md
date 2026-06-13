# ADR 0020: Mandate a write-ahead log with per-entry CRC32C checksums and a trailing mani...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** client
- **Proposed by:** dist_hardliner-07
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Mandate a write-ahead log with per-entry CRC32C checksums and a trailing manifest record containing the HLC timestamp, logical sequence, and blake3 hash of the preceding segment; on startup, validate the entire log forward from the last known-good manifest, truncating any torn writes, and refuse to serve reads until the replay cursor reaches a manifest whose hash chain verifies against the on-disk Merkle root — no "best effort" recovery, no silent corruption.

## Consequences

*To be determined as the architecture is implemented.*

---

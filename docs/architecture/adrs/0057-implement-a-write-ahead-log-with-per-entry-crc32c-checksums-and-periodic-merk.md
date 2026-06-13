# ADR 0057: Implement a write-ahead log with per-entry CRC32C checksums and periodic merk...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** server
- **Proposed by:** dist_hardliner-07
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Implement a write-ahead log with per-entry CRC32C checksums and periodic merkle tree snapshots; every sync ingestion must fsync the WAL before acknowledging, and a background verifier goroutine continuously scans the log for bit rot, emitting signed audit entries on any checksum mismatch with the HLC timestamp and affected key range for forensic replay.

## Consequences

*To be determined as the architecture is implemented.*

---

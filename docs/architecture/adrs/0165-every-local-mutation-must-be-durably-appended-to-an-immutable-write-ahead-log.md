# ADR 0165: Every local mutation must be durably appended to an immutable write-ahead log...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** core
- **Proposed by:** dist_hardliner-07
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Every local mutation must be durably appended to an immutable write-ahead log with HLC timestamps before any in-memory state mutation; the log is the sole source of truth for crash recovery and sync reconciliation, with periodic fsync barriers and checksum verification on every read to detect silent bit rot.

## Consequences

*To be determined as the architecture is implemented.*

---

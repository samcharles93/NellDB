# ADR 0153: Implement a dual-layer write-ahead log: an in-memory ring buffer with CRC32C ...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** core
- **Proposed by:** dist_hardliner-07
- **Net votes:** +3

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Implement a dual-layer write-ahead log: an in-memory ring buffer with CRC32C checksums per record for hot path durability, flushed atomically to a persistent append-only segment file with fsync on configurable fsync intervals and power-loss recovery via monotonic sequence numbers. Reject any mutation whose HLC timestamp regresses beyond a configurable max-drift threshold (default 5s) to prevent clock-skew attacks and stale-write injection during prolonged offline periods. Validate causal consistency on replay by reconstructing the happens-before graph from origin IDs and verifying no cycles exist before applying to the materialized view.

## Consequences

*To be determined as the architecture is implemented.*

---

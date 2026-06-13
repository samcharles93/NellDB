# ADR 0173: Every mutation must be appended to an immutable write-ahead log with HLC time...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** core
- **Proposed by:** dist_hardliner-07
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Every mutation must be appended to an immutable write-ahead log with HLC timestamp and full vector clock of its causal dependencies; no mutation is visible or syncable until its entire causal history is present and validated. Sync protocol must reject any incoming batch that cannot be totally ordered against local log via vector clock dominance — optimistic "last writer wins" without causal proof is a data-loss bug waiting to happen.

## Consequences

*To be determined as the architecture is implemented.*

---

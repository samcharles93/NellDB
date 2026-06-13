# ADR 0055: Implement a segmented, immutable write-ahead log (WAL) per shard where each e...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** server
- **Proposed by:** dist_hardliner-07
- **Net votes:** +3

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Implement a segmented, immutable write-ahead log (WAL) per shard where each entry carries a vector clock capturing full causal dependencies; the replication coordinator must validate that every entry's causal past is fully materialized across a strict write quorum (W > N/2) before advancing the durable frontier, and any replica detecting a gap in its local vector clock must halt ingestion and enter a synchronous repair loop — no speculative execution, no async catch-up, no exceptions.

## Consequences

*To be determined as the architecture is implemented.*

---

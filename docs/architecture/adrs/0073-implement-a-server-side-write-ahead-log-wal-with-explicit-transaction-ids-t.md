# ADR 0073: Implement a server-side write-ahead log (WAL) with explicit transaction IDs t...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** server
- **Proposed by:** enterprise_pragmatist-09
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Implement a server-side write-ahead log (WAL) with explicit transaction IDs that records all incoming mutations before applying LWW resolution, enabling point-in-time recovery and deterministic replay of conflict decisions. Require every sync batch to declare its expected schema version and HLC range, rejecting any batch that falls outside the server's validated schema window or contains causal gaps exceeding a configured threshold. Enforce referential integrity by maintaining a server-side dependency graph of object references, automatically rejecting mutations that would create orphaned vectors or dangling image blobs.

## Consequences

*To be determined as the architecture is implemented.*

---

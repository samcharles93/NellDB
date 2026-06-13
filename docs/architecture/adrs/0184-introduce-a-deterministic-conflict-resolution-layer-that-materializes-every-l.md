# ADR 0184: Introduce a deterministic conflict resolution layer that materializes every L...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** core
- **Proposed by:** enterprise_pragmatist-04
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Introduce a deterministic conflict resolution layer that materializes every LWW decision as an explicit ConflictBranch record (storing loser HLC, origin, payload hash, and resolution timestamp) appended to an immutable audit log; each transaction batch commits atomically by writing a single TransactionCommit record containing the post-apply state hash (BLAKE3 of materialized view), the set of ConflictBranch IDs resolved in this batch, and a Merkle proof linking to the prior TransactionCommit — enabling point-in-time verification, full conflict auditability, and strict transactional boundaries without runtime reflection.

## Consequences

*To be determined as the architecture is implemented.*

---

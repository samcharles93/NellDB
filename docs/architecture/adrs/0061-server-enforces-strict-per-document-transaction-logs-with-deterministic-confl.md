# ADR 0061: Server enforces strict per-document transaction logs with deterministic confl...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** server
- **Proposed by:** enterprise_pragmatist-04
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Server enforces strict per-document transaction logs with deterministic conflict resolution: every sync delta must pass schema validation and explicit state-transition checks before HLC ordering applies, and all LWW resolutions emit an auditable conflict record (winner, loser, vector clock snapshot) to an append-only reconciliation ledger for forensic replay.

## Consequences

*To be determined as the architecture is implemented.*

---

# ADR 0068: Enforce per-collection transactional checkpoints at sync boundaries where all...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** server
- **Proposed by:** enterprise_pragmatist-04
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Enforce per-collection transactional checkpoints at sync boundaries where all pending operations must pass strict schema validation (JSON Schema + custom Go validators) before entering the HLC-ordered commit log; every LWW resolution must write a signed audit entry to an append-only Merkle log capturing pre-state, post-state, losing vector clock, and resolver policy ID for tamper-evident forensics.

## Consequences

*To be determined as the architecture is implemented.*

---

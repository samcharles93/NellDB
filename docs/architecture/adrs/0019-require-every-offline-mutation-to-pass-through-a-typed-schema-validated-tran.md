# ADR 0019: Require every offline mutation to pass through a typed, schema-validated tran...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** client
- **Proposed by:** enterprise_pragmatist-04
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Require every offline mutation to pass through a typed, schema-validated transaction builder that emits a deterministic intent log (not just raw bytes) with explicit preconditions and postconditions — this enables replay-time validation, audit-grade conflict resolution, and prevents silent data corruption when merging week-old offline branches.

## Consequences

*To be determined as the architecture is implemented.*

---

# ADR 0117: Introduce a typed Transaction type with explicit Begin/Commit/Rollback semant...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** architecture
- **Proposed by:** enterprise_pragmatist-04
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Introduce a typed Transaction type with explicit Begin/Commit/Rollback semantics that wraps all mutations (text, vector, image) into atomic units; each Transaction carries a deterministic conflict-resolution policy (LWW-with-audit, application-specific merge, or reject) declared at schema registration time, and the WAL records full before/after state vectors for replayable auditing — no implicit last-write-wins without an explicit policy binding.

## Consequences

*To be determined as the architecture is implemented.*

---

# ADR 0112: Define explicit transaction boundaries around multi-object mutations using a ...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** architecture
- **Proposed by:** enterprise_pragmatist-09
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Define explicit transaction boundaries around multi-object mutations using a deterministic conflict graph: each sync cycle constructs a causally-ordered DAG of pending operations, then applies a two-phase commit with compensating transactions for any detected cycles — ensuring either full atomic application or explicit rollback with audit trail, never silent LWW overwrites.

## Consequences

*To be determined as the architecture is implemented.*

---

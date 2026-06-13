# ADR 0108: Replace naive LWW with a deterministic conflict resolution framework: every c...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** architecture
- **Proposed by:** enterprise_pragmatist-04
- **Net votes:** +2

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Replace naive LWW with a deterministic conflict resolution framework: every conflicting mutation branch is preserved in a per-document CRDT-style merge log with explicit application-level merge functions (registered via schema), and a validated "resolved" state is only committed after passing schema-constrained invariant checks — enabling full audit replay and preventing silent data loss.

## Consequences

*To be determined as the architecture is implemented.*

---

# ADR 0108: Introduce per-collection conflict resolution policies encoded as deterministi...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** architecture
- **Proposed by:** enterprise_pragmatist-04
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Introduce per-collection conflict resolution policies encoded as deterministic, pure Go functions (registered at schema definition time) that receive both conflicting document versions and return a single merged result or explicit rejection; these policies are versioned, audited, and included in the WAL so sync replay yields bit-identical outcomes across all peers without runtime heuristics.

## Consequences

*To be determined as the architecture is implemented.*

---

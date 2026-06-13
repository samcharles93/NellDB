# ADR 0057: Define explicit per-collection conflict resolution policies (LWW, semantic me...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** server
- **Proposed by:** enterprise_pragmatist-04
- **Net votes:** +2

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Define explicit per-collection conflict resolution policies (LWW, semantic merge, or application-defined) with deterministic tie-breaking using HLC + client ID lexicographic ordering, stored as versioned policy documents in the system namespace. Require all sync operations to declare their expected policy version, rejecting writes that target stale or incompatible policy versions to prevent silent semantic drift.

## Consequences

*To be determined as the architecture is implemented.*

---

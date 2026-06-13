# ADR 0005: Implement a per-collection conflict resolution registry where each data type ...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** client
- **Proposed by:** enterprise_pragmatist-04
- **Net votes:** +3

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Implement a per-collection conflict resolution registry where each data type declares a deterministic merge function (e.g., CRDT for counters, operational transform for text, semantic merge for vectors) instead of defaulting to opaque LWW; the registry is versioned alongside schema migrations and produces an auditable conflict resolution log for offline replay verification.

## Consequences

*To be determined as the architecture is implemented.*

---

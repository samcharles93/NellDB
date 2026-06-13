# ADR 0011: Introduce a typed mutation schema with explicit conflict resolution handlers ...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** client
- **Proposed by:** enterprise_pragmatist-04
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Introduce a typed mutation schema with explicit conflict resolution handlers per field type (e.g., CRDT counters for integers, semantic merge for rich text, application-defined resolvers for JSON) — every mutation must declare its conflict branch at write time, and the sync protocol validates the entire causal chain against the local schema before accepting remote writes, rejecting any mutation with undeclared or ambiguous resolution logic.

## Consequences

*To be determined as the architecture is implemented.*

---

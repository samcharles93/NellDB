# ADR 0033: Introduce a compile-time schema registry that generates typed mutation builde...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** client
- **Proposed by:** enterprise_pragmatist-09
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Introduce a compile-time schema registry that generates typed mutation builders and deterministic merge functions per data type, ensuring every offline write carries its schema version hash and explicit conflict resolution logic (e.g., CRDT merge for counters, semantic merge for rich text) rather than deferring to generic LWW. All local transactions must pass through a validated transaction boundary that records intent, schema version, and a deterministic conflict branch ID for later audit and replay. This replaces hand-waving LWW with schema-aware, verifiable conflict resolution that can be unit-tested in isolation.

## Consequences

*To be determined as the architecture is implemented.*

---

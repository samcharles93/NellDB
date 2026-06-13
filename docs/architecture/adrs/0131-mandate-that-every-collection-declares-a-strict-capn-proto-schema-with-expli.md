# ADR 0131: Mandate that every collection declares a strict Cap'n Proto schema with expli...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** architecture
- **Proposed by:** enterprise_pragmatist-09
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Mandate that every collection declares a strict Cap'n Proto schema with explicit per-field conflict resolution policies (LWW, semantic merge, CRDT, or user-defined resolver WASM), and wrap multi-collection writes in a single WAL transaction using a two-phase commit protocol during sync — this yields deterministic conflict branches, schema-enforced validation at write time, and a full audit trail of resolution decisions without trusting peer clocks or relying on opaque LWW.

## Consequences

*To be determined as the architecture is implemented.*

---

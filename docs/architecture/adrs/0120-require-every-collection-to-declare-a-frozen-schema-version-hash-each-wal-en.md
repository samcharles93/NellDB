# ADR 0120: Require every collection to declare a frozen schema version hash; each WAL en...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** architecture
- **Proposed by:** enterprise_pragmatist-09
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Require every collection to declare a frozen schema version hash; each WAL entry carries this hash plus a deterministic conflict resolution proof (Merkle path to resolver WASM bytecode + both conflicting states + resolver output), so any peer can locally re-execute the pure resolver to verify the chosen LWW outcome — turning opaque last-write-wins into auditable, schema-bound state transitions without trusting peer clocks.

## Consequences

*To be determined as the architecture is implemented.*

---

# ADR 0122: Introduce a deterministic Conflict Resolution Ledger (CRL) as a separate appe...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** architecture
- **Proposed by:** enterprise_pragmatist-04
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Introduce a deterministic Conflict Resolution Ledger (CRL) as a separate append-only log alongside the WAL: each conflict detected during sync produces a signed CRL entry containing (pre-state hash, conflicting operations, resolution policy applied, post-state hash, HLC timestamp, resolver identity). Resolution policies are pluggable pure functions (LWW, semantic merge, manual queue) registered at schema definition time per data type, enabling audit replay and strict schema-enforced validation before commit.

## Consequences

*To be determined as the architecture is implemented.*

---

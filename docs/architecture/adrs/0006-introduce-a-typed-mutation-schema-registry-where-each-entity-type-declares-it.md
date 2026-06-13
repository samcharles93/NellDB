# ADR 0006: Introduce a typed mutation schema registry where each entity type declares it...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** client
- **Proposed by:** enterprise_pragmatist-09
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Introduce a typed mutation schema registry where each entity type declares its conflict resolution policy (LWW, deterministic CRDT merge, or explicit user resolution) and validation constraints (schema version, required fields, invariant checks); the sync engine validates every incoming mutation against the registered schema before merge, rejects violations as structured conflict objects with full causal context (DVV + HLC), and writes an immutable audit log entry for every conflict decision — enabling deterministic replay, compliance auditing, and zero-surprise merges.

## Consequences

*To be determined as the architecture is implemented.*

---

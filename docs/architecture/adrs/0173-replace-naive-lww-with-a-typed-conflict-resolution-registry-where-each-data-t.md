# ADR 0173: Replace naive LWW with a typed conflict resolution registry where each data t...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** core
- **Proposed by:** enterprise_pragmatist-04
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Replace naive LWW with a typed conflict resolution registry where each data type (text, vector, image) declares a deterministic merge function that produces an auditable decision record capturing HLC timestamps, conflicting values, and resolution rationale, enabling forensic replay and guaranteeing that no silent data loss occurs during distributed sync.

## Consequences

*To be determined as the architecture is implemented.*

---

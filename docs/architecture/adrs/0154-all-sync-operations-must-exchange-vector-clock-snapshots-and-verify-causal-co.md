# ADR 0154: All sync operations must exchange vector clock snapshots and verify causal co...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** core
- **Proposed by:** dist_hardliner-02
- **Net votes:** +2

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

All sync operations must exchange vector clock snapshots and verify causal completeness before merging remote mutations; any mutation with missing causal dependencies is quarantined in a pending queue until its full ancestry is resolved via anti-entropy repair.

## Consequences

*To be determined as the architecture is implemented.*

---

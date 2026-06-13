# ADR 0117: Introduce a mandatory audit log layer that records every LWW conflict resolut...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** architecture
- **Proposed by:** enterprise_pragmatist-09
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Introduce a mandatory audit log layer that records every LWW conflict resolution decision with the losing write's full payload, HLC timestamps, and causal context before discarding it — enabling point-in-time forensic reconstruction and regulatory compliance without changing the LWW convergence semantics.

## Consequences

*To be determined as the architecture is implemented.*

---

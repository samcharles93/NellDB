# ADR 0167: Implement a mandatory causal barrier protocol: every sync round must compute ...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** core
- **Proposed by:** dist_hardliner-07
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Implement a mandatory causal barrier protocol: every sync round must compute and exchange a causal frontier (vector clock join) before any mutation batch is applied, and the local engine must durably persist the frontier to disk before acknowledging peer receipt; any peer that cannot produce a valid frontier or whose frontier regresses is quarantined until manual intervention — automatic healing of causal gaps is a lie that loses data.

## Consequences

*To be determined as the architecture is implemented.*

---

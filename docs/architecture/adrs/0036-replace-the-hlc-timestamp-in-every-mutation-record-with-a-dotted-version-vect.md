# ADR 0036: Replace the HLC timestamp in every mutation record with a dotted version vect...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** client
- **Proposed by:** dist_hardliner-07
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Replace the HLC timestamp in every mutation record with a dotted version vector (DVV) that tracks per-actor causal history, and require the background sync loop to validate causal precedence before applying any remote mutation — this prevents silent reordering when clocks drift, exposes concurrent branches for explicit LWW tie-breaking, and makes the intent log auditable without trusting wall time.

## Consequences

*To be determined as the architecture is implemented.*

---

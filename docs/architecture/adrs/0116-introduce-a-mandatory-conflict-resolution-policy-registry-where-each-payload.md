# ADR 0116: Introduce a mandatory conflict resolution policy registry where each payload ...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** architecture
- **Proposed by:** enterprise_pragmatist-09
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Introduce a mandatory conflict resolution policy registry where each payload type declares its LWW tie-breaker strategy (e.g., HLC timestamp, then payload hash, then client ID) as a pure function; the sync merger must invoke this registered policy and record the full decision trace (inputs, policy version, output) into an append-only resolution log, ensuring zero hand-waving and full reproducibility for audit.

## Consequences

*To be determined as the architecture is implemented.*

---

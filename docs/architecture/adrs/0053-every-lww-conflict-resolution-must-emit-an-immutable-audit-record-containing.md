# ADR 0053: Every LWW conflict resolution must emit an immutable audit record containing ...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** server
- **Proposed by:** enterprise_pragmatist-04
- **Net votes:** +3

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Every LWW conflict resolution must emit an immutable audit record containing the losing payload, winning HLC timestamp, vector-clock ancestry, and a deterministic hash of the pre-merge state; these audit records are appended to a separate tamper-evident log and included in sync responses so clients can cryptographically verify that no silent data loss occurred during convergence.

## Consequences

*To be determined as the architecture is implemented.*

---

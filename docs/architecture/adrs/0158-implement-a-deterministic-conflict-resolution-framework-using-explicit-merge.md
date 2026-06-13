# ADR 0158: Implement a deterministic conflict resolution framework using explicit merge ...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** core
- **Proposed by:** enterprise_pragmatist-09
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Implement a deterministic conflict resolution framework using explicit merge functions per data type (text CRDT, vector max-pool, image checksum compare) with a mandatory audit log entry for every resolved conflict, including HLC timestamps, peer IDs, and the chosen resolution branch — no silent LWW fallbacks allowed.

## Consequences

*To be determined as the architecture is implemented.*

---

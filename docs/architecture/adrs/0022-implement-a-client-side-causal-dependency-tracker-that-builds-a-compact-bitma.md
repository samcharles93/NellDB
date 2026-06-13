# ADR 0022: Implement a client-side causal dependency tracker that builds a compact bitma...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** client
- **Proposed by:** dist_hardliner-07
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Implement a client-side causal dependency tracker that builds a compact bitmap frontier of cross-collection mutation dependencies during offline operation: each mutation declares its causal prerequisites (collection + key + HLC) as a 16-byte fixed slot in the WAL record (extending ID-0274), and the sync protocol validates the entire dependency DAG atomically against the server's Merkle state (from ID-0325) before any LWW merge — violations surface as structured causal conflict objects requiring explicit resolution, not silent overwrites.

## Consequences

*To be determined as the architecture is implemented.*

---

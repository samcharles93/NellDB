# ADR 0129: Replace HLC-only ordering with dotted version vectors (DVV) per key, embedded...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** architecture
- **Proposed by:** dist_hardliner-07
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Replace HLC-only ordering with dotted version vectors (DVV) per key, embedded in a hash-chained WAL where each entry carries a Merkle mountain range (MMR) root for O(log n) range proofs; on sync, peers exchange DVV heads and MMR peaks to compute the exact causal frontier, then stream only missing entries with inclusion proofs — this eliminates LWW data loss entirely, detects forked histories from clock drift or Byzantine peers, and allows clients to verify server state without trusting any single timestamp.

## Consequences

*To be determined as the architecture is implemented.*

---

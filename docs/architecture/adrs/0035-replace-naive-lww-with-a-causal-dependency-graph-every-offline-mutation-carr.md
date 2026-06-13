# ADR 0035: Replace naive LWW with a causal dependency graph: every offline mutation carr...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** client
- **Proposed by:** dist_hardliner-02
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Replace naive LWW with a causal dependency graph: every offline mutation carries a per-entity version vector (not just HLC) in the WAL, enabling detection of concurrent edits that LWW would silently drop; sync performs three-way merge with explicit conflict markers for true concurrency, and a Merkle hash chain over WAL segments provides O(log n) integrity verification against bit-flip corruption during week-long offline storage.

## Consequences

*To be determined as the architecture is implemented.*

---

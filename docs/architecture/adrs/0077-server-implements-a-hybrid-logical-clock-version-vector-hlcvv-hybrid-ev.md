# ADR 0077: Server implements a Hybrid Logical Clock + Version Vector (HLC+VV) hybrid: ev...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** server
- **Proposed by:** dist_hardliner-02
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Server implements a Hybrid Logical Clock + Version Vector (HLC+VV) hybrid: every mutation carries a dotted version vector (DVV) alongside its HLC, enabling the server to detect and reject causally impossible operations (e.g., a delete that precedes its create in the causal graph) even when client clocks drift arbitrarily. The append-only WAL is segmented into immutable, content-addressed chunks (blake3 hash as filename) with a manifest merkle tree; sync responses include merkle proofs for the requested range, allowing clients to cryptographically verify they received a contiguous, untampered slice of the causal history. A background "causal garbage collector" computes the global stable frontier across all replicas and prunes DVV dots only when every replica has acknowledged them, preventing premature GC during network partitions.

## Consequences

*To be determined as the architecture is implemented.*

---

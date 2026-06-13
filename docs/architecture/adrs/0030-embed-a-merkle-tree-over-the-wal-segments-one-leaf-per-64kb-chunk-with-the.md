# ADR 0030: Embed a merkle tree over the WAL segments (one leaf per 64KB chunk) with the ...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** client
- **Proposed by:** dist_hardliner-07
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Embed a merkle tree over the WAL segments (one leaf per 64KB chunk) with the root hash committed into every HLC timestamped mutation record; on sync, the client and server exchange merkle proofs for divergent ranges instead of full replays, enabling O(log n) anti-entropy and immediate detection of silent bit-flip corruption or server-side history rewrites — the tree is append-only, updated incrementally via a fixed-size cache of internal nodes in a pre-allocated []byte arena, and verifiable in WASM without allocations using only stdlib hash/crc32 or crypto/sha256.

## Consequences

*To be determined as the architecture is implemented.*

---

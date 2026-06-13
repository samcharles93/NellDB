# ADR 0010: Implement an immutable write-ahead log (WAL) with per-mutation causal hashes ...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** client
- **Proposed by:** dist_hardliner-07
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Implement an immutable write-ahead log (WAL) with per-mutation causal hashes (blake3 of parent hashes + payload) stored alongside each entry; on replay, validate the entire causal chain before applying state, rejecting any forked or truncated history. Sync checkpoints must include a Merkle root of the verified prefix so remote peers can audit log integrity without full replay.

## Consequences

*To be determined as the architecture is implemented.*

---

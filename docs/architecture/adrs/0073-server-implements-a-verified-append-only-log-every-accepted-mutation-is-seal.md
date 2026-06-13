# ADR 0073: Server implements a verified append-only log: every accepted mutation is seal...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** server
- **Proposed by:** dist_hardliner-02
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Server implements a verified append-only log: every accepted mutation is sealed into a merkle-tree-backed WAL entry containing the HLC timestamp, client ID, payload hash, and the previous entry's merkle root; the server signs each root with a long-term Ed25519 key rotated via a separate append-only key log. Clients verify the full causal chain on sync by fetching merkle proofs for any range — no trust in server ordering, no consensus protocol required, and a compromised server cannot rewrite history without detection.

## Consequences

*To be determined as the architecture is implemented.*

---

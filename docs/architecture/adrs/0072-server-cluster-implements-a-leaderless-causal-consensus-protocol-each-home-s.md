# ADR 0072: Server cluster implements a leaderless causal consensus protocol: each home s...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** server
- **Proposed by:** dist_hardliner-02
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Server cluster implements a leaderless causal consensus protocol: each home server replicates mutations via quorum write (W+R > N) with HLC+DVV causal metadata, and emits a signed "causal receipt" for every accepted mutation containing the DVV, global causal frontier hash, and a merkle proof linking to the last epoch checkpoint. Clients verify receipts locally and gossip them to detect server-side forks or history rewriting — any server presenting a receipt chain with a divergent merkle root is cryptographically provable as Byzantine, triggering automatic client failover to honest replicas.

## Consequences

*To be determined as the architecture is implemented.*

---

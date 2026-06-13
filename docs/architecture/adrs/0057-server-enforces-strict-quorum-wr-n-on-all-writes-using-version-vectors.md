# ADR 0057: Server enforces strict quorum (W+R > N) on all writes using version vectors (...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** server
- **Proposed by:** dist_hardliner-02
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Server enforces strict quorum (W+R > N) on all writes using version vectors (not HLC alone) to capture true causal ordering; each shard runs a background anti-entropy loop that compares merkle trees across replicas every 60s and repairs divergent ranges via streaming hash-exchange — no read-repair on hot path. Clients must present a client-side observed clock drift bound (max 500ms) with each sync batch; server rejects batches exceeding drift threshold and forces client re-sync with authoritative HLC from quorum.

## Consequences

*To be determined as the architecture is implemented.*

---

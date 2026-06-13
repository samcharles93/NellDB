# ADR 0161: Every sync cycle must produce a Merkle proof of the local WAL segment's causa...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** core
- **Proposed by:** dist_hardliner-02
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Every sync cycle must produce a Merkle proof of the local WAL segment's causal closure, verified against the remote's vector clock frontier before any LWW resolution occurs; divergent forks are quarantined in an immutable conflict log with explicit human-in-the-loop resolution — no automatic "last writer wins" without cryptographic proof of causal precedence.

## Consequences

*To be determined as the architecture is implemented.*

---

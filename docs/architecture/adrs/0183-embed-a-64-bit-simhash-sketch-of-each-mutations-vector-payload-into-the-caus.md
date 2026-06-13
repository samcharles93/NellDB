# ADR 0183: Embed a 64-bit SimHash sketch of each mutation's vector payload into the caus...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** core
- **Proposed by:** embedding_zealot-08
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Embed a 64-bit SimHash sketch of each mutation's vector payload into the causal column's fingerprint slot, replacing the BLAKE3 parent-hash with a xor-merged sketch of immediate parents; this turns causal dominance checks into O(1) Hamming-distance comparisons that simultaneously verify ancestry and expose semantic locality — mutations with similar vectors naturally cluster in sketch space, enabling spatial sync reconciliation that only exchanges relevant vector neighborhoods instead of full causal histories.

## Consequences

*To be determined as the architecture is implemented.*

---

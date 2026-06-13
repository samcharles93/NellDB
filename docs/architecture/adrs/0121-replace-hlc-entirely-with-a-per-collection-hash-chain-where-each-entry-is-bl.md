# ADR 0121: Replace HLC entirely with a per-collection hash-chain where each entry is [BL...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** architecture
- **Proposed by:** dist_hardliner-07
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Replace HLC entirely with a per-collection hash-chain where each entry is [BLAKE3(prev_hash || key || value || deps_hash)][varint key_len][key][varint val_len][val][varint deps_count][deps_hashes...] — deps_hashes encodes the causal frontier as a compact set of entry hashes (not vector clocks), enabling exact causal ordering verification during sync via Merkle anti-entropy on the chain tips; conflicts resolve by comparing causal depth then lexicographic hash, guaranteeing deterministic convergence without trusting any clock, peer, or network ordering, all in a single append-only []byte with zero background goroutines.

## Consequences

*To be determined as the architecture is implemented.*

---

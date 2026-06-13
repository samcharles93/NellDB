# ADR 0101: Mandate that every write operation appends an immutable, cryptographically ch...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** architecture
- **Proposed by:** dist_hardliner-02
- **Net votes:** +4

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Mandate that every write operation appends an immutable, cryptographically chained entry to a per-collection WAL (hash-linked via BLAKE3), where each entry commits the full document state, its HLC timestamp, and a causal dependency vector clock; replication streams must transmit the WAL tail and verify chain integrity before applying, guaranteeing that any peer can reconstruct exact state at any HLC snapshot and detect silent corruption or reordering attacks.

## Consequences

*To be determined as the architecture is implemented.*

---

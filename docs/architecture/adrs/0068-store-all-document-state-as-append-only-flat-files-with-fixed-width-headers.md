# ADR 0068: Store all document state as append-only flat files with fixed-width headers (...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** server
- **Proposed by:** wasm_purist-06
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Store all document state as append-only flat files with fixed-width headers (HLC timestamp, doc ID, payload length) and raw payload bytes — no indexes, no WAL, no compaction. Resolve conflicts at read time by scanning the tail N records per doc ID using a memory-mapped byte slice and a single linear pass; the server never holds more than one document's history in memory at once.

## Consequences

*To be determined as the architecture is implemented.*

---

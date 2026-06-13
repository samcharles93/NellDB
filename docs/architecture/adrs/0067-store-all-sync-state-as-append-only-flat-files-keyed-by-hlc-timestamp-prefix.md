# ADR 0067: Store all sync state as append-only flat files keyed by HLC timestamp (prefix...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** server
- **Proposed by:** wasm_purist-01
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Store all sync state as append-only flat files keyed by HLC timestamp (prefix-sorted), with LWW resolution reduced to a single memcmp on the timestamp prefix — no indexes, no metadata, just mmap'd byte slices and binary search for range queries.

## Consequences

*To be determined as the architecture is implemented.*

---

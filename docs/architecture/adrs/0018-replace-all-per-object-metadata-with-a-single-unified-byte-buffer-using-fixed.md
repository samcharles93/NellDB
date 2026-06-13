# ADR 0018: Replace all per-object metadata with a single unified byte buffer using fixed...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** client
- **Proposed by:** wasm_purist-06
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Replace all per-object metadata with a single unified byte buffer using fixed-offset headers: each record starts with a 16-byte header (8B HLC timestamp, 4B payload length, 4B type tag) followed by raw payload bytes; text, vectors, and images share one contiguous arena allocated once at startup, eliminating all dynamic allocations and serialization overhead during the offline week.

## Consequences

*To be determined as the architecture is implemented.*

---

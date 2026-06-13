# ADR 0058: Server handles all vector similarity search and complex indexing; clients rec...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** server
- **Proposed by:** wasm_purist-01
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Server handles all vector similarity search and complex indexing; clients receive only raw document bytes and minimal HLC-ordered sync deltas — no graph structures, no float32 arrays, no in-memory indexes ever ship to WASM.

## Consequences

*To be determined as the architecture is implemented.*

---

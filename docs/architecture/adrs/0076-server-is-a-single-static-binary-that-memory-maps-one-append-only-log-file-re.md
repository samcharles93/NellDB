# ADR 0076: Server is a single static binary that memory-maps one append-only log file re...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** server
- **Proposed by:** wasm_purist-06
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Server is a single static binary that memory-maps one append-only log file read-only; all sync, compaction, and indexing happen client-side via HTTP Range requests. Zero goroutines, zero allocations, zero background tasks — the server process only answers GET /log?offset=&len= and HEAD /log requests. This keeps the Go-to-WASM binary under 20KB and shifts all CPU/memory cost to clients who can afford it.

## Consequences

*To be determined as the architecture is implemented.*

---

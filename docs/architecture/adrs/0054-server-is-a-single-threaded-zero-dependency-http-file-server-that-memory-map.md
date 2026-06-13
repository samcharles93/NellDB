# ADR 0054: Server is a single-threaded, zero-dependency HTTP file server that memory-map...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** server
- **Proposed by:** wasm_purist-06
- **Net votes:** +3

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Server is a single-threaded, zero-dependency HTTP file server that memory-maps one append-only log file read-only and answers only GET /log?offset=N&len=M and HEAD /log — no HLC validation, no monotonic checks, no indexing, no sync.Map, no channels, no writer goroutine, no background tasks. Clients bear full responsibility for HLC ordering, conflict resolution, and binary search via Range requests; the server binary compiles to ~15KB WASM (stdlib net/http + runtime/cgo only) and uses zero heap allocations per request after warmup.

## Consequences

*To be determined as the architecture is implemented.*

---

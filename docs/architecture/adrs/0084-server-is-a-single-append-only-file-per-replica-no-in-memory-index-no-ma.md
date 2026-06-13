# ADR 0084: Server is a single append-only file per replica — no in-memory index, no ma...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** server
- **Proposed by:** wasm_purist-06
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Server is a single append-only file per replica — no in-memory index, no map, no WAL segmentation. Clients sync via HTTP Range requests over a flat byte stream: each record is fixed-width [u64 HLC][u32 doc_id][u32 payload_len][payload...]. Compaction is client-driven; the server only truncates its tail on explicit client DELETE /log?before=HLC, keeping the Go binary at ~30KB WASM with zero allocations in the hot path.

## Consequences

*To be determined as the architecture is implemented.*

---

# ADR 0082: Build the sync server entirely with stdlib: net/http for the wire protocol, s...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** server
- **Proposed by:** gopher_fundamentalist-05
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Build the sync server entirely with stdlib: net/http for the wire protocol, sync.RWMutex + map for the in-memory HLC-indexed log, os + encoding/binary for the append-only WAL, and context for cancellation — zero external deps, ~50KB WASM. Clients push/pull opaque byte blobs via a single /sync endpoint that streams newline-delimited HLC+payload records; the server only enforces monotonically increasing HLC per key and truncates the log at a configurable horizon. Conflict resolution stays client-side; the server is a dumb, durable, horizontally-scalable log replicator.

## Consequences

*To be determined as the architecture is implemented.*

---

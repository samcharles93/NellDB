# ADR 0052: Server sync protocol uses pure net/http with binary encoding/binary for HLC t...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** server
- **Proposed by:** gopher_fundamentalist-10
- **Net votes:** +3

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Server sync protocol uses pure net/http with binary encoding/binary for HLC timestamps and LWW payloads — no gRPC, protobuf, or external deps. Conflict resolution runs in a single goroutine per document key using sync.Map and time.Time comparison, with changes fanned out via standard channels to connected clients.

## Consequences

*To be determined as the architecture is implemented.*

---

# ADR 0118: Build the sync engine entirely with Go stdlib primitives: use `sync.

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** architecture
- **Proposed by:** gopher_fundamentalist-05
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Build the sync engine entirely with Go stdlib primitives: use `sync.Map` for concurrent document state, `encoding/binary` + `crypto/sha256` for deterministic HLC payload hashing, and a `time.Ticker` driven goroutine with bounded channels for offline mutation queuing and background sync — zero external dependencies, compiles to minimal WASM via `GOOS=js GOARCH=wasm`.

## Consequences

*To be determined as the architecture is implemented.*

---

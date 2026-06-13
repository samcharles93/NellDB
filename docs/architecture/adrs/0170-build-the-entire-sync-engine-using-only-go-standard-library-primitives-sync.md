# ADR 0170: Build the entire sync engine using only Go standard library primitives: `sync.

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** core
- **Proposed by:** gopher_fundamentalist-10
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Build the entire sync engine using only Go standard library primitives: `sync.RWMutex` for concurrent state access, `encoding/binary` + `encoding/json` for deterministic wire encoding, `crypto/sha256` for content-addressable integrity, `time` + atomic counters for HLC timestamps, and `io/fs` abstractions for pluggable storage backends — zero external dependencies, compiles cleanly to WASM via TinyGo.

## Consequences

*To be determined as the architecture is implemented.*

---

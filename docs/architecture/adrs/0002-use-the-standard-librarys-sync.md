# ADR 0002: Use the standard library's sync.

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** client
- **Proposed by:** gopher_fundamentalist-10
- **Net votes:** +3

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Use the standard library's sync.Mutex and a ring buffer backed by a pre-allocated []byte slice for the offline write-ahead log; serialize operations with encoding/binary into fixed-width records so the log is trivially mmap-friendly on desktop and memcpy-free in WASM. Drive background sync with a single goroutine per collection owned by a context.WithCancel, communicating via unbuffered chan struct{} ticks from time.Ticker — no external deps, zero allocations after warmup.

## Consequences

*To be determined as the architecture is implemented.*

---

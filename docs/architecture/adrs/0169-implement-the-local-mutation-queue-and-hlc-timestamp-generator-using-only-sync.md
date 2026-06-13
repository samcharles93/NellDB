# ADR 0169: Implement the local mutation queue and HLC timestamp generator using only sync.

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** core
- **Proposed by:** gopher_fundamentalist-05
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Implement the local mutation queue and HLC timestamp generator using only sync.Mutex, atomic.Int64, and unbuffered channels from the standard library — no external clock or queue packages. The sync loop reads from a single outbound channel, batches mutations by HLC wall-time buckets, and writes LWW-resolved batches to the WAL via io.WriteAt on a pre-allocated *os.File, keeping the entire hot path allocation-free and WASM-compatible.

## Consequences

*To be determined as the architecture is implemented.*

---

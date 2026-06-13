# ADR 0014: Implement the local document store as a single `sync.

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** client
- **Proposed by:** gopher_fundamentalist-05
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Implement the local document store as a single `sync.RWMutex`-guarded `map[string]*Document` where each Document holds a `[]byte` payload, HLC timestamp, and version vector; persist mutations via a dedicated goroutine that drains a buffered `chan WALEntry` into a `bufio.Writer`/`os.File` using `encoding/binary` + `hash/crc32` (IEEE table) for zero-dependency durability, with `sync.Pool` recycling entry buffers to eliminate allocation pressure during week-long offline operation.

## Consequences

*To be determined as the architecture is implemented.*

---

# ADR 0125: Implement a lock-free dual-buffer WAL using sync/atomic.

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** architecture
- **Proposed by:** gopher_fundamentalist-05
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Implement a lock-free dual-buffer WAL using sync/atomic.Pointer[[]byte] (Go 1.19+) for zero-contention writes: the active buffer accumulates length-prefixed entries encoded with encoding/binary and protected by hash/crc32 checksums; a single background goroutine swaps buffers atomically when full, persists the sealed buffer via bufio.Writer to os.File, and signals completion through a stdlib channel. On sync, stream sealed buffers over net.Conn using io.CopyBuffer with a sync.Pooled byte slice, verifying CRC32 on receipt; HLC is a single uint64 updated via atomic.CompareAndSwap. Zero mutexes, zero external deps, compiles to ~4KB WASM.

## Consequences

*To be determined as the architecture is implemented.*

---

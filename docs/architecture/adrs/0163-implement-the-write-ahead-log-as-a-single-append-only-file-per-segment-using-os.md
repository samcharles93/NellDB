# ADR 0163: Implement the write-ahead log as a single append-only file per segment using os.

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** core
- **Proposed by:** gopher_fundamentalist-10
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Implement the write-ahead log as a single append-only file per segment using os.Create, bufio.Writer, and encoding/binary for fixed-width HLC timestamp + payload length headers; recover on startup by scanning sequentially with io.ReaderAt—no external WAL libraries, no mmap, just the standard library.

## Consequences

*To be determined as the architecture is implemented.*

---

# ADR 0117: Build the sync engine around a single `sync.

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** architecture
- **Proposed by:** gopher_fundamentalist-10
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Build the sync engine around a single `sync.Mutex` protected in-memory index (map[string][]byte) with a standard library `bufio.Writer`-backed WAL per collection; use plain `time.Time` with a monotonic `sync/atomic` counter for HLC, and resolve conflicts via LWW by comparing `(wallTime, counter, nodeID)` tuples — no vector clocks, no external deps, just `encoding/binary`, `hash/crc32`, and `os.File.Sync()` for durability.

## Consequences

*To be determined as the architecture is implemented.*

---

# ADR 0107: Use sync/atomic for HLC timestamp generation, bare channels for offline mutat...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** architecture
- **Proposed by:** gopher_fundamentalist-10
- **Net votes:** +2

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Use sync/atomic for HLC timestamp generation, bare channels for offline mutation queues, and stdlib net/http with context for server sync — no external deps, pure Go primitives compiling cleanly to WASM via syscall/js.

## Consequences

*To be determined as the architecture is implemented.*

---

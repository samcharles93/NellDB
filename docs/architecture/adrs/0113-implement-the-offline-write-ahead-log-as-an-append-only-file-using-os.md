# ADR 0113: Implement the offline write-ahead log as an append-only file using os.

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** architecture
- **Proposed by:** gopher_fundamentalist-10
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Implement the offline write-ahead log as an append-only file using os.File with sync/atomic for sequence numbers and encoding/binary for frame encoding; drive the background sync with a single goroutine per peer using net/http and context.Context for cancellation, and use sync.Pool with byte slices for zero-copy frame reuse during WASM-compiled vector quantization — all stdlib, zero CGO, tinygo compatible.

## Consequences

*To be determined as the architecture is implemented.*

---

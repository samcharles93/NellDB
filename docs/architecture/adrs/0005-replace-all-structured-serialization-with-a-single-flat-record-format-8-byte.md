# ADR 0005: Replace all structured serialization with a single flat record format: 8-byte...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** client
- **Proposed by:** wasm_purist-01
- **Net votes:** +2

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Replace all structured serialization with a single flat record format: 8-byte HLC | 2-byte key length | 2-byte payload length | raw key bytes | raw payload bytes — no varints, no CBOR, no reflection. The entire sync state is a single uint64 file offset cursor; the WASM binary stays under 40KB by compiling with -ldflags="-s -w" and using only unsafe.Pointer casts over a pre-allocated []byte arena — zero allocations, zero parsing, pure memcpy.

## Consequences

*To be determined as the architecture is implemented.*

---

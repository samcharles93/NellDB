# ADR 0052: Server exposes a minimal append-only log of opaque byte blobs keyed by HLC ti...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** server
- **Proposed by:** wasm_purist-06
- **Net votes:** +5

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Server exposes a minimal append-only log of opaque byte blobs keyed by HLC timestamp — no schema registry, no semantic merge, no per-document metadata beyond a single u64 HLC. Conflict resolution is strict byte-level LWW only; clients opt into higher-level CRDTs or semantic merge locally, keeping the core sync protocol wire format to ~200 bytes per operation and the Go-to-WASM compile target under 100KB.

## Consequences

*To be determined as the architecture is implemented.*

---

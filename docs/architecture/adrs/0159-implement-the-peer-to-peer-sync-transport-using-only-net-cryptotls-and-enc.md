# ADR 0159: Implement the peer-to-peer sync transport using only net, crypto/tls, and enc...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** core
- **Proposed by:** gopher_fundamentalist-10
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Implement the peer-to-peer sync transport using only net, crypto/tls, and encoding/binary from the standard library: a length-prefixed frame protocol (4-byte big-endian length + 1-byte frame type + payload) over mutual-TLS connections, where each frame carries a batch of HLC-ordered mutations encoded as flat byte slices. Peers exchange HLC watermarks and a compact Merkle root (crypto/sha256) of their mutation log tails to detect divergence in O(log n) round-trips, then stream missing frames via a simple goroutine-per-connection pipeline with channel-based backpressure — no protobuf, no gRPC, no external dependencies, identical code path for WASM (via net/http) and native.

## Consequences

*To be determined as the architecture is implemented.*

---

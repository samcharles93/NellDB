# ADR 0103: Use a single Go routine per client with a standard library ring buffer (backe...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** architecture
- **Proposed by:** gopher_fundamentalist-10
- **Net votes:** +3

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Use a single Go routine per client with a standard library ring buffer (backed by a pre-allocated []byte slice) for the local WAL, encoding entries as length-prefixed CBOR via encoding/binary — no Merkle trees, no vector clocks. On reconnect, drain the buffer through a standard net.Conn using a simple HLC timestamp + clientID tuple for LWW ordering; conflicts resolve server-side by comparing HLC wall-time then clientID lexicographically.

## Consequences

*To be determined as the architecture is implemented.*

---

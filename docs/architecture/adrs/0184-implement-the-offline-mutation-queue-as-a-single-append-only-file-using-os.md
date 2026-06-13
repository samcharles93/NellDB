# ADR 0184: Implement the offline mutation queue as a single append-only file using os.

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** core
- **Proposed by:** gopher_fundamentalist-10
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Implement the offline mutation queue as a single append-only file using os.OpenFile with O_APPEND|O_CREATE|O_WRONLY, where each record is length-prefixed via binary.PutVarint and protected by a trailing CRC32C (hash/crc32); on startup, replay validates checksums and truncates at the first corruption using io/fs.ReadDir and os.Truncate — zero external deps, survives process death, and the file descriptor stays open for the app lifetime so fsync() calls are explicit stdlib syscalls. The sync client uses net.Dialer with a custom net.Resolver (pure Go) and a single net.Conn per peer, framing mutations as length-delimited protobuf-free byte slices written via io.WriteString and read via io.ReadFull into a reusable []byte pool — backpressure emerges naturally from TCP flow control and the Go runtime's network poller, no channels needed.

## Consequences

*To be determined as the architecture is implemented.*

---

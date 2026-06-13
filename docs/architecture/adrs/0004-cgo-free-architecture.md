# ADR 0004: CGO-Free Architecture

- **Status:** Accepted
- **Date:** 2026-06-07

## Context

Nell-engine must compile to WebAssembly (`GOOS=js GOARCH=wasm`) for browser and Electron runtimes. The Go WASM target does not support CGO — any C dependency in the dependency tree breaks compilation with a linker error.

This rules out:
- `mattn/go-sqlite3` (CGO required for the C SQLite library).
- Any library wrapping a C library (e.g. RocksDB, LMDB bindings).
- Platform-specific C code for compression, crypto, or IO.

The client is Go code that compiles to WASM. The server is the same Go code compiled natively. Both must use the same core engine without conditional CGO features.

## Decision

**Zero CGO dependencies. No exceptions.**

All code in the engine must be pure Go, verified by:
- `CGO_ENABLED=0 go build ./...` passing in CI.
- `GOOS=js GOARCH=wasm go build ./...` passing in CI.
- No `import "C"` anywhere in the dependency tree.

This applies to:
- **Storage:** Pure-Go KV stores only. No SQLite, RocksDB, LMDB, or any C-backed engine.
- **Compression:** Pure-Go compression libraries (e.g. `compress/gzip` from stdlib, `klauspost/compress` for Snappy/Zstd).
- **Networking:** Go's `net` package, WebSocket libraries in pure Go.
- **Serialisation:** Stdlib `encoding/json`, or pure-Go binary formats.

The `internal/buildtags/` directory can be used for platform-specific optimisations (e.g. IndexedDB bindings on WASM) but must never introduce CGO.

## Consequences

### Positive

- Flawless cross-compilation to every Go target including WASM.
- Single codebase for server and client — no feature-gating behind CGO.
- Fast compilation times (no C toolchain involved).
- Trivially reproducible builds.

### Negative

- Some operations (vector search, compression) may be slower without C-accelerated libraries.
- Cannot use well-established C-based embedded databases (SQLite, LMDB) directly.

### Mitigations

- Pure-Go alternatives exist for most needs: `bbolt` (KV store), `klauspost/compress` (zstd/snappy), pure-Go WebSocket libraries.
- For PoC performance targets, pure Go is more than sufficient. C-accelerated paths can be added behind build tags later if needed.

---

**Enforced by:** CI build matrix testing `CGO_ENABLED=0` and `GOOS=js GOARCH=wasm`.

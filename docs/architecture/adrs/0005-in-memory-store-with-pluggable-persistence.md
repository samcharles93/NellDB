# ADR 0005: In-Memory Store with Pluggable Persistence

- **Status:** Accepted
- **Date:** 2026-06-07

## Context

The engine needs a storage layer that works in two fundamentally different environments:
- **Native Go (server/desktop):** The store persists to disk for durability across restarts.
- **WASM (browser/Electron):** The store lives in memory and optionally backs to IndexedDB via JS interop.

These environments have incompatible filesystem APIs. A single storage implementation cannot cover both. However, the sync engine, conflict resolver, and replication logic should not care about the underlying storage mechanism — they operate on records.

## Decision

Define a `Store` interface that abstracts all storage operations:

```go
type Store interface {
    Put(incoming Record) (accepted bool, current Record, err error)
    Get(id string) (Record, error)
    List() ([]Record, error)
    GetChangesSince(clock HLC) ([]Record, error)
    Close() error
}
```

Provide an in-memory implementation (`MemoryStore`) as the default for both runtimes:

```go
type MemoryStore struct {
    mu      sync.RWMutex
    records map[string]Record
}
```

`MemoryStore` is:
- Thread-safe (via `sync.RWMutex`).
- Valid for PoC on both server and WASM.
- Ephemeral — data is lost on restart.

Future `Store` implementations can wrap:
- **Server:** A persistent pure-Go KV store (e.g. `bbolt`) under the same interface.
- **WASM:** An IndexedDB-backed store via `syscall/js` interop.

The sync engine, HTTP handlers, and replication logic all take a `Store` parameter and never depend on the concrete implementation.

## Consequences

### Positive

- Storage is swappable without touching the sync or replication layer.
- `MemoryStore` is trivially correct and debuggable — ideal for PoC.
- The interface is small (5 methods) and easy to implement for any backend.

### Negative

- `MemoryStore` loses data on restart — unsuitable for production without a persistent backend.
- `GetChangesSince` on a large dataset requires iterating all records and filtering by clock (O(n) per query). A persistent backend can optimise this with a clock-indexed bucket.

### Mitigations

- The PoC ships with `MemoryStore`. A `BboltStore` wrapping `go.etcd.io/bbolt` is the natural next step and shares the same interface.
- `GetChangesSince` performance is acceptable for PoC scale (thousands of records, linear scan). Future work can add an HLC-indexed side bucket.

---

**Implementation:** `core/store.go` — `MemoryStore` struct.

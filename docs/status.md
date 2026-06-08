# NellDB — Project Status

NellDB is a distributed, real-time, document-oriented database. JSON-native, HTTP-synced, embeddable as a Go library or a JavaScript client. The Go module is `github.com/samcharles93/NellDB`.

## Package layout

```
github.com/samcharles93/NellDB          → package nell    (core types, Store interface, MemoryStore, LWW)
github.com/samcharles93/NellDB/logstore  → package logstore (persistent Zstd-compressed store)
github.com/samcharles93/NellDB/server    → package server   (HTTP sync, anti-entropy)
github.com/samcharles93/NellDB/sdk       → package sdk      (document API + Replicator)
github.com/samcharles93/NellDB/client    → package client   (WASM runtime + JS SDK, build: js && wasm)
```

The product is **NellDB**.  The Go module path (`NellDB`) is the
repository name and is what you import; the package names are the short
forms listed above.  `import "github.com/samcharles93/NellDB"` gives
you the engine; `import "github.com/samcharles93/NellDB/sdk"` gives
you the document API.

## What Was Built

### Core Engine (`package nell`, root)

| Component | File | Status |
|---|---|---|
| **Hybrid Logical Clock** | `types.go` | HLC with `Tick()` for local writes and `Update()` for peer clock merging. Deterministic `GreaterThan()` ordering. |
| **Record type** | `types.go` | Universal document: `ID`, `Type` (text/vector/image), `Payload []byte`, `Vector []float32`, `Clock HLC`, `UpdatedBy`, `Deleted` tombstone. |
| **Store interface** | `store.go` | Abstract storage: `Put`, `PutLocal`, `Get`, `Delete`, `List`, `GetChangesSince`, `Close`. |
| **MemoryStore** | `store.go` | Thread-safe in-memory implementation. Default for WASM client and ephemeral server mode. |
| **LWW conflict resolution** | `store.go` | `ResolveConflict(local, incoming)` — higher HLC wins, lexical `UpdatedBy` tie-break. |
| **KnowledgeVector** | `store.go` | `map[NodeID]HLC` — tracks highest seen clock per peer for anti-entropy sync. |

### Persistent Storage (`logstore/`)

| Component | File | Status |
|---|---|---|
| **LogStore** | `logstore/log.go` | Custom append-only storage engine. Zstd-compressed binary frames. Replays on startup to rebuild in-memory index. Crash-safe (append+fsync, torn last frame ignored). Zero storage engine dependencies. |

Frame format: `[uncompressed_len u32][compressed_len u32][Zstd data]`

### Server (`server/`)

| Endpoint | Method | Purpose |
|---|---|---|
| `/sync/pull` | POST | Client sends `{since: HLC}`, server returns all records with a higher clock. |
| `/sync/push` | POST | Client sends `{changes: [Record]}`, server applies via LWW and broadcasts to peers. |
| `/sync/check` | POST | Peer sends `{sender_node_id, vector: KnowledgeVector}`, server returns records the peer is missing. Anti-entropy. |

Server can be embedded as a library (`server.New(store, nodeID)`) or run standalone via `cmd/nelldb-server`.

### CLI (`cmd/nelldb-server/`)

```bash
nelldb-server --addr :8080 --node-id my-server --data nell.db
nelldb-server --in-memory  # ephemeral mode for testing
```

### WASM Client + JS SDK (`client/`)

| Callback | Method | SDK method |
|---|---|---|
| `nellPut` | Insert/update via LWW | `db.put({id, type, payload})` |
| `nellGet` | Fetch by ID | `db.get(id)` |
| `nellDelete` | Tombstone | `db.delete(id)` |
| `nellList` | All non-deleted | `db.list()` |

Build: `go generate ./client/...` → `nell.wasm` + `wasm_exec.js`.

JS SDK also has `db.sync(serverUrl)` with pull-then-push flow and lifecycle hooks (`onConnect`, `onDisconnect`, `onConflict`, `onSyncComplete`).

### Tests

```
store_test.go        — 35 tests: HLC, LWW, KnowledgeVector, MemoryStore (edge/stress/concurrency)
logstore/log_test.go  — 15 tests: persistence, replay, corruption, concurrency
server/server_test.go — 14 tests: HTTP handlers, invalid input, anti-entropy edge cases
sdk/docdb_test.go     — 17 tests: CRUD, conflict, AllDocs, changes feed, replication
```

Verified end-to-end: push records → server stores → kill server → restart → pull returns same records.

---

## What's Next

### Near-term improvements

1. **Compaction** — `LogStore` grows unbounded. Tombstones and overwritten records accumulate. Need a background compaction that rewrites the log keeping only the latest version of each record.

2. **Outbox log on client** — Currently the JS SDK's `sync()` pushes ALL local records. Should track only pending mutations in an outbox, deduplicate by record ID, and clear after server ack.

3. **WebSocket real-time push** — Server broadcasts mutations to connected peers/clients via WebSocket. Currently the broadcast skeleton exists but isn't wired.

4. **Peer discovery** — mDNS for LAN auto-discovery. Static seed peers for WAN. Peer state machine (active/degraded/dead) with heartbeat loop.

5. **Server-to-server anti-entropy loop** — Background goroutine that periodically calls `/sync/check` on random peers using the KnowledgeVector. Currently the endpoint exists but nothing calls it.

6. **WASM client persistence** — `MemoryStore` in WASM is ephemeral. Need `IndexedDBStore` implementing `nell.Store` via `syscall/js` interop so client data survives page reloads.

7. **Conflict callbacks in SDK** — `onConflict` hook exists but never fires. LWW silently overwrites. Should surface conflicts so apps can react.

8. **Better clock serialization** — HLC is currently `{wall_time, counter}` as two JSON fields. A single 64-bit + 32-bit binary encoding would be smaller on the wire.

### Medium-term features

9. **Binary wire protocol** — JSON is wasteful for sync batches. CBOR or a custom flat-buffer encoding would reduce per-message overhead significantly, especially for image payloads.

10. **Tombstone GC** — Tombstones accumulate forever. Need a configurable TTL (default 30 days) after which tombstones are dropped during compaction.

11. **Per-collection namespaces** — Currently a flat key space. Collections/namespaces would let users isolate data (e.g. "notes", "images", "settings").

12. **Read-only replicas** — A server that syncs inbound but doesn't accept local writes. Useful for backup nodes.

13. **Auth layer** — No authentication on sync endpoints. Need token-based auth at minimum for production use.

### Longer-term / nice-to-have

14. **Vector search** — Cosine similarity linear scan exists in concept. HNSW-lite index for sub-millisecond ANN search at scale. Not critical for initial use cases.

15. **LZ4 compression option** — Zstd is adjustable; at speed-priority levels it rivals LZ4. But a pure LZ4 mode for extreme-throughput workloads could be added.

16. **Delta sync** — Currently syncs full records. For large text documents, operational-transform or diff-based deltas would reduce bandwidth.

17. **Plug-in conflict resolvers** — LWW is simple but blunt. Per-type merge functions (CRDT counters, OT for text, semantic merge for vectors) registered at schema definition time.

---

## Dependencies

```
github.com/klauspost/compress  (Zstd — pure Go, CGO-free, WASM-compatible)
```

That's it. One external dependency. Everything else is the Go standard library.

---

## Quick Start

```bash
# Build and run the server
go build -o nelldb-server ./cmd/nelldb-server/
./nelldb-server --data my-data.db

# Push a record
curl -X POST http://localhost:8080/sync/push \
  -d '{"changes":[{"id":"hello","type":"text","payload":"d29ybGQ=","clock":{"wall_time":1,"counter":0},"updated_by":"cli"}]}'

# Pull all records
curl -X POST http://localhost:8080/sync/pull \
  -d '{"since":{"wall_time":0,"counter":0}}'

# Build WASM client
go generate ./client/...
```

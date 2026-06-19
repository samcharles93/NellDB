# NellDB — Project Status

NellDB is a distributed, real-time, document-oriented database. JSON-native at the SDK layer, binary on the wire, embeddable as a Go library or a JavaScript client. The Go module is `github.com/samcharles93/NellDB`.

## Package layout

```
github.com/samcharles93/NellDB          → package nell    (core types, Store interface, MemoryStore, LWW, indexes)
github.com/samcharles93/NellDB/logstore  → package logstore (persistent Zstd-compressed store, configurable flush/compression)
github.com/samcharles93/NellDB/server    → package server   (HTTP sync, anti-entropy, WebSocket, mDNS, metrics)
github.com/samcharles93/NellDB/sdk       → package sdk      (DocDB, MVCC _rev, changes feed, Replicator)
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
| **Hybrid Logical Clock** | `types.go` | HLC with `Tick()` for local writes and `Update()` for peer clock merging. Deterministic `GreaterThan()` ordering. Binary encoding (`EncodeBinary`/`DecodeBinary`, 12 bytes). |
| **Record type** | `types.go` | Universal document: `Collection`, `ID`, `Type` (text/vector/image), `Payload []byte`, `Vector []float32`, `Clock HLC`, `UpdatedBy`, `Deleted` tombstone. Binary `MarshalBinary`/`UnmarshalBinary`. |
| **Store interface** | `store.go` | Abstract storage: `Put`, `PutLocal`, `Get`, `Delete`, `List`, `ListAll`, `Query`, `GetChangesSince`, `SearchSimilar`, `NodeID`, `Close`. |
| **MemoryStore** | `store.go` | Thread-safe in-memory implementation with collection index + HLC index. Default for WASM client and ephemeral server mode. |
| **LWW conflict resolution** | `store.go` | `ResolveConflict(local, incoming)` — higher HLC wins, lexical `UpdatedBy` tie-break. Pure and deterministic. |
| **KnowledgeVector** | `store.go` | `map[NodeID]HLC` — tracks highest seen clock per peer for anti-entropy sync. Binary marshal/unmarshal. |
| **CosineSimilarity** | `vector.go` | Vector similarity with 1BRC-style parallel scan. |
| **HMAC signing** | `sign.go` | `SignBody` for sync endpoint auth. |

### Persistent Storage (`logstore/`)

| Component | File | Status |
|---|---|---|
| **LogStore** | `log.go` | Custom append-only storage engine. Zstd-compressed binary frames. Parallel replay on startup (1BRC-style worker pool). Crash-safe (append-only, torn last frame ignored). |
| **Collection index** | `log.go` | `map[collection]set[key]` — makes `List`/`ListAll` O(collection size). |
| **HLC index** | `log.go` | Lazily-rebuilt sorted `[]clockKey` — makes `GetChangesSince` O(log n + k). |
| **Compaction** | `log.go` | Rewrites log from in-memory index (no file rescan). Drops old tombstones. Auto-compaction on background ticker. |
| **OpenLogWithOptions** | `log.go` | Configurable `FlushInterval` (group commit) and `CompressionLevel`. |

Frame format: `[uncompressed_len u32][compressed_len u32][Zstd data]`

### SDK (`sdk/`)

| Component | File | Status |
|---|---|---|
| **DocDB** | `docdb.go` | Document API: `Put`, `Get`, `GetMany`, `Delete`, `AllDocs`, `DocCount`. MVCC `_rev` tokens (content-hash). Stale-write detection via `ErrConflict`. |
| **Changes feed** | `changes.go` | Best-effort streaming of document changes. Drops when subscriber buffers fill. |
| **Replicator** | `replicate.go` | `Pull` (via `/sync/check` anti-entropy), `Push` (via `/sync/bin/push` binary), `Live` (HTTP polling), `LiveWS` (WebSocket with auto-reconnect). HMAC auth support. |
| **Replication metadata** | `meta.go`, `vector.go` | `meta:clock` and `meta:vector` stored as ordinary records, filtered from replication payloads. |

### Server (`server/`)

| Endpoint | Method | Protocol | Purpose |
|---|---|---|---|
| `/sync/pull` | POST | JSON | Client sends `{since: HLC}`, server returns all records with a higher clock. |
| `/sync/push` | POST | JSON | Client sends `{changes: [Record]}`, server applies via LWW and broadcasts. |
| `/sync/check` | POST | JSON | Anti-entropy: peer sends KnowledgeVector, server returns missing records. |
| `/sync/bin/check` | POST | Binary | Same as `/sync/check` — binary encoding, length-prefixed streaming. |
| `/sync/bin/push` | POST | Binary | Same as `/sync/push` — binary encoding. |
| `/sync/ws` | GET | WebSocket | Real-time mutation broadcast to connected peers/clients. |
| `/health` | GET | JSON | Health probe (no auth). |
| `/ready` | GET | JSON | Readiness probe (no auth). |

| Component | File | Status |
|---|---|---|
| **MeshManager** | `peer.go` | Server-to-server anti-entropy. Reconciles with all active peers per tick (max 4 concurrent). `TrackedPeer` state machine (Active/Degraded/Dead) with heartbeat loop. |
| **mDNS discovery** | `discovery.go` | Advertises `_nell-core._tcp`. Auto-discovers LAN peers. Gracefully degrades on platforms without multicast. |
| **HMAC auth** | `auth.go` | Middleware wrapping `/sync/*` routes with `X-Nell-Timestamp` / `X-Nell-Signature` verification. |
| **Metrics** | `metrics.go` | Prometheus/OpenTelemetry metrics: push/pull counts, replication lag, peer state. |
| **TLS** | `main.go` | Optional TLS via `--tls-cert` / `--tls-key` flags. |

Server can be embedded as a library (`server.New(store, nodeID)`) or run standalone via `cmd/nelldb-server`.

### CLI (`cmd/nelldb-server/`)

```bash
nelldb-server --addr :9342 --node-id my-server --data nell.db
nelldb-server --in-memory                          # ephemeral mode
nelldb-server --discovery                          # mDNS peer discovery
nelldb-server --auth-secret shared-secret          # HMAC auth
nelldb-server --tls-cert cert.pem --tls-key key.pem # TLS
```

Config via `nell.yaml` (storage flush interval, compression level, compaction interval, tombstone TTL, sync batch size, vector index settings).

### WASM Client + JS SDK (`client/`)

| Callback | Method | SDK method |
|---|---|---|
| `nellPut` | Insert/update via LWW | `db.put({id, type, payload})` |
| `nellGet` | Fetch by ID | `db.get(id)` |
| `nellDelete` | Tombstone | `db.delete(id)` |
| `nellList` | All non-deleted | `db.list()` |

Uses `IndexedDBStore` (persistent via IndexedDB) with `MemoryStore` fallback. Build: `go generate ./client/...` → `nell.wasm` + `wasm_exec.js`.

### Tests

```
package nell        — 64 tests: HLC, LWW, KnowledgeVector, MemoryStore, indexes, vectors (edge/stress/concurrency)
package logstore    — 33 tests: persistence, replay, corruption, compaction, indexes, group commit, concurrency
package server      — 131 tests: HTTP handlers, binary sync, anti-entropy, WebSocket, auth, edge cases
package sdk         — 43 tests: CRUD, MVCC _rev, AllDocs, changes feed, replication, conflicts
package client      — 2 tests: WASM integration (Node + wasm_exec.js)
package cmd/server  — 8 tests: build, vet, config loading
Total: 281 tests
```

Verified end-to-end: push records → server stores → kill server → restart → pull returns same records. WASM integration tested via `make test-wasm` (builds `client/main.go` for `js/wasm` and executes under Node).

### Benchmarks

| Benchmark | Throughput | Notes |
|---|---|---|
| In-memory writes (1M records) | ~245K ops/s | `examples/perf/` |
| Durable writes (1M records) | ~51K ops/s | `examples/perf-persist/` — LogStore, per-write flush |
| Range scan (10K from 1M, in-memory) | 241 ms | Collection index |
| Range scan (1K from 1M, durable) | 231 ms | Collection index |
| Binary sync (100K records) | ~44K docs/s | `examples/sync-bench/` |
| Recovery (1M records) | ~900 ms | Parallel replay |

---

## What's Next

### Near-term

1. **HNSW + Product Quantization** — Config scaffolding exists (`vector.enable_hnsw`, `vector.pca_dimensions`, `vector.pq_subspaces`, `vector.pq_centroids`). Implement the HNSW-lite index with quantized int8 vectors for sub-millisecond ANN search at scale. Currently `SearchSimilar` is a linear scan.

2. **Pre-normalized vectors** — Store a normalized copy of each vector on write so `CosineSimilarity` becomes a single dot product. Halves the FLOPs.

3. **Outbox log on client** — The JS SDK's `sync()` pushes all local records. Should track only pending mutations in an outbox, deduplicate by record ID, and clear after server ack.

4. **WASM sync** — The client side still has TODOs around full sync behavior. The Go SDK Replicator is complete; the JS SDK needs parity.

### Medium-term

5. **Read-only replicas** — A server that syncs inbound but doesn't accept local writes. Useful for backup nodes.

6. **Delta sync** — Currently syncs full records. For large text documents, diff-based deltas would reduce bandwidth.

7. **Plug-in conflict resolvers** — LWW is simple but blunt. Per-type merge functions (CRDT counters, OT for text, semantic merge for vectors) registered at schema definition time.

8. **fsync on demand** — Neither per-write flush nor group commit calls `fsync`. Expose an explicit `Sync()` method for power-loss durability.

---

## Dependencies

```
github.com/klauspost/compress   (Zstd — pure Go, CGO-free, WASM-compatible)
github.com/gorilla/websocket    (WebSocket real-time sync)
github.com/hashicorp/mdns       (mDNS LAN peer discovery)
github.com/prometheus/client_golang + go.opentelemetry.io/otel  (metrics)
gopkg.in/yaml.v3                (config file parsing)
```

---

## Quick Start

```bash
# Build and run the server
go build -o nelldb-server ./cmd/nelldb-server/
./nelldb-server --data nell.db

# Push a record (binary endpoint)
curl -X POST http://localhost:9342/sync/push \
  -H "Content-Type: application/json" \
  -d '{"changes":[{"id":"hello","type":"text","payload":"d29ybGQ=","clock":{"wall_time":1,"counter":0},"updated_by":"cli"}]}'

# Pull all records
curl -X POST http://localhost:9342/sync/pull \
  -H "Content-Type: application/json" \
  -d '{"since":{"wall_time":0,"counter":0}}'

# Build WASM client
go generate ./client/...

# Run the SDK tour
go run examples/tour/main.go

# Run benchmarks
go run examples/perf/main.go          # in-memory
go run examples/perf-persist/main.go  # durable
go run examples/sync-bench/main.go    # sync throughput
```
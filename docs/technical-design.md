# NellDB — Technical Design

## 1. Overview

NellDB is a distributed, real-time, document-oriented database.
JSON-native at the SDK layer, binary on the wire, embeddable as a Go
library or a JavaScript client.  One Go codebase compiles to three
targets: a native server binary, a WASM client runtime, and a
JavaScript SDK wrapper.

**Why it exists:** the data layer in an offline-first, real-time app
should not require a managed sync service, a separate daemon, or two
engines to maintain.  NellDB makes the server a Go library you import
and the client a JS class you import.  Both share a single Go codebase
and a single on-disk format.

---

## 2. Architecture Layers

```
┌─────────────────────────────────────────────┐
│  JS SDK (nell.js)                           │  ← hand-written wrapper
│  - NellDB class: put, get, query, delete    │
│  - sync lifecycle: onConnect, onConflict    │
│  - WASM loader: init(wasmUrl)               │
├─────────────────────────────────────────────┤
│  WASM Bridge (client/main.go)               │  ← //go:build js && wasm
│  - syscall/js callbacks: nellPut, nellGet   │
│  - JSON marshal/unmarshal across boundary   │
├─────────────────────────────────────────────┤
│  Core Engine (package nell)                 │  ← shared between all runtimes
│  - Record, HLC, DataType                    │
│  - Store interface                          │
│  - MemoryStore (in-memory, indexed)         │
│  - ResolveConflict (LWW)                    │
│  - CosineSimilarity (vector search)         │
│  - KnowledgeVector                          │
├─────────────────────────────────────────────┤
│  Storage Backends (implement Store)         │
│  - LogStore (Zstd append-only frame log)    │
│  - MemoryStore (in-memory, indexed)         │
│  - IndexedDBStore (WASM, native key ranges) │
├─────────────────────────────────────────────┤
│  SDK (sdk/)                                 │  ← application layer
│  - DocDB: MVCC _rev, AllDocs, changes feed  │
│  - Replicator: Push/Pull/Live/LiveWS        │
├─────────────────────────────────────────────┤
│  Server Runtime (server/)                   │  ← native Go binary
│  - HTTP API: /sync/check, /sync/push,       │
│    /sync/pull, /sync/bin/check,             │
│    /sync/bin/push, /sync/ws                 │
│  - HMAC auth, TLS, metrics                  │
│  - MeshManager: anti-entropy + heartbeat    │
│  - mDNS peer discovery                      │
└─────────────────────────────────────────────┘
```

---

## 3. Data Model

### 3.1 Record

```go
type DataType string

const (
    TypeText   DataType = "text"
    TypeVector DataType = "vector"
    TypeImage  DataType = "image"
)

type HLC struct {
    WallTime int64 `json:"wall_time"` // Unix milliseconds
    Counter  int32 `json:"counter"`   // per-millisecond monotonic tick
}

type Record struct {
    Collection string    `json:"collection"`
    ID         string    `json:"id"`
    Type       string    `json:"type"`
    Payload    []byte    `json:"payload,omitempty"`
    Vector     []float32 `json:"vector,omitempty"`
    Clock      HLC       `json:"clock"`
    UpdatedBy  string    `json:"updated_by"`
    Deleted    bool      `json:"deleted"`
}
```

- `Collection` scopes records into named namespaces (default: `"default"`). The composite key is `collection:id`.
- `Type` is a discriminator. New types can be added — text, vector, and image are the seed set.
- `Payload` carries text (UTF-8) or image bytes. The application layer encodes/decodes.
- `Vector` is a dedicated `[]float32` field so similarity scans don't unmarshal Payload.
- `Clock` provides causal ordering (see §5).
- `UpdatedBy` is the node ID that last mutated the record. Used in conflict resolution (§6) as the deterministic tie-breaker.
- `Deleted` is an explicit tombstone. Deleted records propagate through sync and are eventually compacted.

### 3.2 Binary Encoding

Records use a custom binary encoding for persistence and the high-performance sync endpoints. The layout is:

```
[1 byte: version]
[1 byte: deleted (0/1)]
[12 bytes: HLC clock]
[2 bytes: collection len] [N bytes: collection]
[2 bytes: id len]          [N bytes: id]
[2 bytes: type len]        [N bytes: type]
[2 bytes: updatedBy len]   [N bytes: updatedBy]
[4 bytes: vector count]    [N*4 bytes: float32 vector data]
[4 bytes: payload len]     [N bytes: payload]
```

This encoding is used by `Record.MarshalBinary`/`UnmarshalBinary` for:
- LogStore frame format (Zstd-compressed)
- `/sync/bin/check` and `/sync/bin/push` endpoints (length-prefixed streaming)

The legacy JSON endpoints (`/sync/pull`, `/sync/push`, `/sync/check`) still use JSON for backwards compatibility, but the binary endpoints are the primary high-throughput path.

### 3.3 LogStore Frame Format

Each record is persisted as a length-prefixed Zstd-compressed frame:

```
[4 bytes: uncompressed_len (big-endian uint32)]
[4 bytes: compressed_len   (big-endian uint32)]
[compressed_len bytes: Zstd-compressed binary record]
```

The format is append-only and crash-safe — a torn frame at the tail is ignored on replay. Startup replays the entire log in parallel (1BRC-style worker pool) to rebuild the in-memory index.

---

## 4. Storage Engine

### 4.1 Store Interface

```go
type Store interface {
    Put(incoming Record) (accepted bool, current Record, err error)
    PutLocal(rec *Record) (Record, error)
    Get(collection, id string) (Record, error)
    Delete(collection, id string) (Record, error)
    List(collection string) ([]Record, error)
    ListAll(collection string) ([]Record, error) // includes tombstones
    Query(q Query) ([]Record, error)
    GetChangesSince(clock HLC) ([]Record, error)
    SearchSimilar(collection string, vector []float32, limit int) ([]Record, error)
    NodeID() string
    Close() error
}
```

Everything above this interface — the sync engine, conflict resolver, HTTP handlers, SDK — operates on `Store`, never on a concrete backend.

### 4.2 Implementations

| Backend | Target | Persistent? | Notes |
|---|---|---|---|
| `MemoryStore` | All (default for tests) | No | `sync.RWMutex` + `map[string]Record`. Collection index + HLC index for sublinear scans. |
| `LogStore` | Native server | Yes | Append-only Zstd-compressed binary log. Parallel replay. Collection index + HLC index. Configurable flush and compression. |
| `IndexedDBStore` | WASM client | Yes | Wraps browser IndexedDB via `syscall/js`. Uses native `IDBKeyRange` queries for collection-scoped reads. |

### 4.3 LogStore

`logstore.OpenLog(path, nodeID)` opens (or creates) a LogStore. `logstore.OpenLogWithOptions(path, nodeID, opts)` adds two knobs:

- **`FlushInterval`**: 0 (default) flushes after every write (process-crash safe, one syscall per write). >0 enables group commit — a background goroutine flushes on the interval, trading up to that much latency on crash for ~1.5x write throughput.
- **`CompressionLevel`**: Zstd encoder level (Fastest/Default/Better/Best). `SpeedFastest` roughly halves encode time vs `SpeedDefault` at a modest ratio cost.

Neither mode calls `fsync` — durability against power loss requires a separate fsync (e.g. on Close or Compaction).

### 4.4 Secondary Indexes

Both `MemoryStore` and `LogStore` maintain two secondary indexes:

1. **Collection index** (`map[collection]set[key]`): makes `List`/`ListAll` O(collection size) instead of O(total records). Maintained incrementally on every write (O(1)).
2. **HLC index** (lazily-rebuilt `[]clockKey` sorted by HLC): makes `GetChangesSince` O(log n + k) instead of O(n). Writes set a dirty flag; the next query rebuilds if needed.

`IndexedDBStore` uses native `IDBKeyRange.bound` queries, which serve the same purpose.

### 4.5 Compaction

`LogStore.Compact(tombstoneThreshold)` rewrites the log file to reclaim space:
- Keeps only the latest version of each record (from the in-memory index, no file rescan)
- Drops tombstones older than the threshold
- Writes to a temp file, then atomically renames

Auto-compaction runs on a background ticker (`StartCompaction`), configured via `nell.yaml`.

### 4.6 IndexedDB Layout

```
Database: NellDB
  ObjectStore: records (keyPath: "collection:id")
    List/ListAll use IDBKeyRange.bound(collection+":", collection+":\uffff")
```

---

## 5. Ordering: Hybrid Logical Clocks

### 5.1 Why HLC

Clients can be offline for a week. Wall clocks drift. NTP is not guaranteed on mobile or Electron. Pure logical clocks (Lamport) solve ordering but produce timestamps unrelated to real time. Vector clocks capture causality but grow with the cluster.

HLC combines both: a physical wall time (for human-readable ordering and catch-up) with a logical counter (for causal ordering of events in the same millisecond). 12 bytes. Converges to physical time when nodes communicate.

### 5.2 Algorithm

```
On local write (Tick):
  now = system.Now().UnixMilli()
  if now > local.WallTime:
    local.WallTime = now
    local.Counter = 0
  else:
    local.Counter++

On receiving a peer message with clock peerClock (Update):
  if peerClock.WallTime > local.WallTime:
    local.WallTime = peerClock.WallTime
    local.Counter = peerClock.Counter + 1
  else if peerClock.WallTime == local.WallTime && peerClock.Counter >= local.Counter:
    local.Counter = peerClock.Counter + 1
```

`GreaterThan` is lexical: compare WallTime first, then Counter.

### 5.3 Guarantee

If event B happened-after event A on any device, `B.Clock.GreaterThan(A.Clock)` is true. This holds across network partitions because the counter increments monotonically within each wall-time tick.

---

## 6. Conflict Resolution: Last-Write-Wins

### 6.1 Algorithm

```go
func ResolveConflict(local, incoming *Record) *Record {
    if incoming.Clock.GreaterThan(local.Clock) {
        return incoming
    }
    if local.Clock.GreaterThan(incoming.Clock) {
        return local
    }
    // Deterministic tie-break: lexical node ID comparison
    if incoming.UpdatedBy > local.UpdatedBy {
        return incoming
    }
    return local
}
```

This function is pure and deterministic. Every node that applies the same inputs reaches the same conclusion.

There are two conflict layers:
- **Local stale-write detection**: the SDK's `_rev` check (see §7).
- **Cross-node convergence**: engine-level LWW using HLC first and lexical `UpdatedBy` as the deterministic tie-break.

### 6.2 Tombstones

A deletion sets `Deleted: true` with the current HLC. The tombstone propagates through sync like any other record. When a tombstone arrives with a clock higher than the local record, the local record is marked deleted. Tombstones are compacted after a configurable horizon (default: 7 days / 168 hours).

---

## 7. SDK Layer (package sdk)

### 7.1 DocDB

`DocDB` is the application-facing document layer. It maps user `sdk.Doc` (a `map[string]any`) onto engine `nell.Record` values.

- **MVCC `_rev`**: each document version gets a content-hash rev token (`1-<sha1>`). Stale writes are rejected with `ErrConflict`.
- **`AllDocs`**: prefix-scoped document listing.
- **Changes feed**: best-effort streaming of document changes (drops when subscriber buffers fill — use `AllDocs`/replication for a complete view).
- **Reserved fields**: `_id`, `_rev`, `_deleted`. Everything else is application data and round-trips unchanged.
- **Replication metadata**: stored as ordinary records with synthetic IDs `meta:clock` and `meta:vector`, filtered out of replication payloads.

### 7.2 Replicator

`sdk.Replicator` is the replication path for Go clients:

- **`Pull`**: uses `/sync/check` (anti-entropy via KnowledgeVector), not `/sync/pull`. Per-peer knowledge vectors survive concurrent writes from different nodes.
- **`Push`**: uses `/sync/bin/push` (binary) for throughput.
- **`Live`**: HTTP polling on an interval.
- **`LiveWS`**: WebSocket push-based sync with automatic reconnect and exponential-backoff jitter.
- **HMAC auth**: `SetAuthSecret()` signs all HTTP requests with `X-Nell-Timestamp` / `X-Nell-Signature` headers.

---

## 8. Client Architecture (Offline-First)

### 8.1 Local Store

The WASM client runs `IndexedDBStore` (or `MemoryStore` as fallback). All reads and writes hit the local store first. There is no network round-trip for a write to succeed.

### 8.2 WASM Bridge

`client/main.go` (build tag `js,wasm`) registers global JS callbacks (`nellPut`, `nellGet`, `nellDelete`, `nellList`) backed by the store. `client/nell.js` is a thin wrapper around those callbacks. `main()` blocks on `<-ch` to keep the WASM instance alive.

---

## 9. Server Architecture

### 9.1 Deployment Modes

| Mode | Description |
|---|---|
| **Standalone** | One server process. Accepts client syncs. No peer replication. |
| **Distributed mesh** | Multiple server processes. Peer-to-peer replication via anti-entropy + WebSocket broadcast. |

Same binary. Configured via a `peers` list in `nell.yaml` or mDNS discovery (`--discovery` flag).

### 9.2 Sync Endpoints

| Endpoint | Protocol | Purpose |
|---|---|---|
| `POST /sync/check` | JSON | Anti-entropy: client sends KnowledgeVector, server returns missing records as JSON |
| `POST /sync/bin/check` | Binary | Same as `/sync/check` but uses binary encoding + length-prefixed streaming |
| `POST /sync/push` | JSON | Client sends a batch of records, server applies via LWW |
| `POST /sync/bin/push` | Binary | Same as `/sync/push` but binary-encoded |
| `POST /sync/pull` | JSON | Client sends a single HLC, server streams all records newer than that |
| `GET /sync/ws` | WebSocket | Real-time push: server broadcasts mutations to connected peers |
| `GET /health` | JSON | Health probe (no auth) |
| `GET /ready` | JSON | Readiness probe (no auth) |

Binary endpoints (`/sync/bin/*`) use `Record.MarshalBinary` with `[4-byte length prefix][record bytes]` framing. These are the primary high-throughput path used by `sdk.Replicator`.

All `/sync/*` routes (except health/ready) are wrapped with HMAC auth when a secret is configured.

### 9.3 Knowledge Vector

```go
type KnowledgeVector map[string]HLC
```

Each node tracks the highest HLC it has seen from every other node (identified by `UpdatedBy`). The vector is:
- Updated on every `Put()`: `vector[record.UpdatedBy] = max(vector[record.UpdatedBy], record.Clock)`.
- Exchanged during anti-entropy to compute deltas.
- Persisted as a synthetic record (`meta:vector`) in the store.

### 9.4 MeshManager

`server.MeshManager` (`server/peer.go`) handles server-to-server reconciliation:

- Reconciles with all active peers per tick (max 4 concurrent) instead of one random peer.
- `TrackedPeer` state machine: Active → Degraded → Dead, with background heartbeat (`HEAD /health` every 10s).
- Dead peers are excluded from reconciliation and broadcast.

### 9.5 Peer Discovery

| Method | Scope | How |
|---|---|---|
| **mDNS** | LAN | Advertise `_nell-core._tcp` service. Browse for peers on same subnet. Gracefully degrades on platforms without multicast (Docker, WSL). |
| **Seed peers** | WAN | Configured static list in `nell.yaml`. |

---

## 10. Vector Search

### 10.1 Strategy

Linear scan with Cosine Similarity. Pure Go, no dependencies, compiles to WASM.

```go
func CosineSimilarity(a, b []float32) float32 {
    // dot product / (norm(a) * norm(b))
}
```

`SearchSimilar` leverages 1BRC-style parallelism to saturate CPU cores during the scan. Candidates are filtered by collection and type before the parallel scan.

### 10.2 Future: HNSW + PQ

The config supports `vector.enable_hnsw`, `vector.pca_dimensions`, `vector.pq_subspaces`, and `vector.pq_centroids` for a future HNSW + product quantization index. This is scoped for post-PoC.

---

## 11. Compression

### 11.1 LogStore Compression

All LogStore frames are compressed with Zstd (`klauspost/compress/zstd`) — a pure-Go, CGO-free, WASM-compatible implementation. The compression level is configurable via `Options.CompressionLevel`.

### 11.2 Wire Compression

Sync batches over HTTP may be gzip-compressed (`Content-Encoding: gzip`). Binary endpoints use `application/octet-stream` with length-prefixed framing.

---

## 12. Safety & Transactions

### 12.1 Local Write Atomicity

A `Put()` operation holds the store mutex for the duration of the write: in-memory index update + append to the buffered writer. The in-memory map and the log are updated atomically from the caller's perspective.

### 12.2 Crash Recovery

LogStore is append-only. On restart, the log is replayed frame-by-frame (in parallel) to rebuild the in-memory index. A torn frame at the tail (partial write during crash) is detected by header/length checks and silently skipped. No WAL replay or page recovery needed — the append-only design is inherently crash-safe.

Group-commit mode (`FlushInterval > 0`) may lose the last flush interval of writes on a process crash. Torn-tail handling on replay copes with partial frames.

### 12.3 IndexedDB

`IndexedDBStore` relies on IndexedDB's transaction durability (browser-managed).

---

## 13. Build System

### 13.1 `go:generate`

`client/generate.go`:

```go
//go:generate env GOOS=js GOARCH=wasm go build -ldflags="-s -w" -o nell.wasm main.go
//go:generate cp $GOROOT/misc/wasm/wasm_exec.js .
```

Running `go generate ./client/...` produces `client/nell.wasm` and `client/wasm_exec.js`.

### 13.2 Makefile Targets

```makefile
build-wasm:   go generate ./client/...
build-server: go build -o bin/nelldb-server ./cmd/nelldb-server/
test-wasm:    # builds WASM + runs under Node via wasm_exec.js
```

---

## 14. Project Directory Structure

```
NellDB/
├── types.go              # package nell — Record, HLC, DataType
├── store.go              # package nell — Store interface, MemoryStore, ResolveConflict, KnowledgeVector, indexes
├── vector.go             # package nell — CosineSimilarity
├── config.go             # package nell — Config, LoadConfig, DefaultConfig
├── sign.go               # package nell — HMAC signing for sync auth
├── logstore/
│   ├── log.go            # LogStore — append-only Zstd frame log, indexes, compaction
│   └── log_test.go       # Tests: persistence, replay, corruption, compaction, indexes
├── sdk/
│   ├── doc.go            # Doc, Change, DocRange types
│   ├── docdb.go          # DocDB — document API (MVCC _rev, AllDocs, changes)
│   ├── replicate.go      # Replicator — Push/Pull/Live/LiveWS
│   ├── meta.go           # Persisted meta:clock
│   ├── vector.go         # Persisted meta:vector (KnowledgeVector)
│   ├── rev.go            # _rev generation (sha1 tokens)
│   └── changes.go        # Changes feed hub
├── server/
│   ├── main.go           # Server, HTTP handlers, binary sync endpoints
│   ├── peer.go           # MeshManager, TrackedPeer, anti-entropy
│   ├── discovery.go      # mDNS peer discovery
│   ├── metrics.go        # Prometheus metrics
│   ├── auth.go           # HMAC auth middleware
│   └── webui.go          # Optional web dashboard
├── client/
│   ├── main.go           # WASM entrypoint + syscall/js callbacks
│   ├── generate.go       # go:generate directives
│   └── nell.js           # JS SDK wrapper
├── cmd/nelldb-server/
│   └── main.go           # Server CLI entrypoint
├── examples/
│   ├── tour/             # End-to-end SDK tour
│   ├── perf/             # In-memory throughput benchmark
│   ├── perf-persist/     # Durable (LogStore) throughput benchmark
│   ├── sync/             # Sync demo
│   ├── sync-bench/       # Sync throughput benchmark
│   └── vector/           # Vector search demo
├── docs/
│   ├── technical-design.md
│   └── status.md
├── go.mod
├── Makefile
└── nell.yaml             # Default server config
```

---

## 15. Configuration

Server configuration is loaded from `nell.yaml` (overridable via CLI flags):

```yaml
server:
  port: 9342
  data_dir: "."
  max_skew_ms: 500

storage:
  flush_interval_ms: 0       # 0 = per-write flush (safe), >0 = group commit
  compression_level: "default" # fastest|default|better|best

compaction:
  interval_minutes: 60
  tombstone_ttl_hours: 168   # 7 days

sync:
  max_batch_size: 1000
  staleness_eviction_days: 14

discovery:
  enabled: false

vector:
  enable_hnsw: true
  pca_dimensions: 128
  pq_subspaces: 16
  pq_centroids: 256
```
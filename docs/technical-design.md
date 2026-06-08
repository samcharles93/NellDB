# NellDB — Technical Design

## 1. Overview

NellDB is a distributed, real-time, document-oriented database.
JSON-native, HTTP-synced, embeddable as a Go library or a JavaScript
client.  One Go codebase compiles to three targets: a native server
binary, a WASM client runtime, and a JavaScript SDK wrapper.

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
│  Core Engine (core/)                        │  ← shared between all runtimes
│  - Record, HLC, DataType                    │
│  - Store interface                          │
│  - MemoryStore (in-memory, thread-safe)     │
│  - ResolveConflict (LWW)                    │
│  - CosineSimilarity (vector search)         │
├─────────────────────────────────────────────┤
│  Storage Backend (implements Store)         │
│  - Server: BboltStore (go.etcd.io/bbolt)    │
│  - WASM:   IndexedDBStore (syscall/js)      │
├─────────────────────────────────────────────┤
│  Server Runtime (server/)                   │  ← native Go binary
│  - HTTP API: /sync/check, /sync/push        │
│  - WebSocket realtime fan-out               │
│  - PeerManager: gossip + anti-entropy       │
│  - MeshRegistry: mDNS + seed peers          │
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
    ID        string    `json:"id"`
    Type      DataType  `json:"type"`
    Payload   []byte    `json:"payload,omitempty"`
    Vector    []float32 `json:"vector,omitempty"`
    Clock     HLC       `json:"clock"`
    UpdatedBy string    `json:"updated_by"`
    Deleted   bool      `json:"deleted"`
}
```

- `Type` is a discriminator. New types can be added — text, vector, and image are the seed set.
- `Payload` carries text (UTF-8) or image bytes. The application layer encodes/decodes.
- `Vector` is a dedicated `[]float32` field so similarity scans don't unmarshal Payload.
- `Clock` provides causal ordering (see §5).
- `UpdatedBy` is the node ID that last mutated the record. Used in conflict resolution (§6) as the deterministic tie-breaker.
- `Deleted` is an explicit tombstone. Deleted records propagate through sync and are eventually compacted.

### 3.2 Wire Format

All records serialise to/from JSON. The WASM bridge receives JSON strings from JS and unmarshals them into Go structs. The sync protocol sends records as JSON arrays.

Future: a binary CBOR or flat-buffer wire format to reduce per-message overhead for large sync batches.

---

## 4. Storage Engine

### 4.1 Store Interface

```go
type Store interface {
    Put(incoming Record) (accepted bool, current Record, err error)
    Get(id string) (Record, error)
    List() ([]Record, error)
    GetChangesSince(clock HLC) ([]Record, error)
    Delete(id string) error
    Close() error
}
```

Everything above this interface — the sync engine, conflict resolver, HTTP handlers — operates on `Store`, never on a concrete backend.

### 4.2 Implementations

| Backend | Target | Persistent? | Notes |
|---|---|---|---|
| `MemoryStore` | All (default) | No | `sync.RWMutex` + `map[string]Record`. Used for PoC and tests. |
| `BboltStore` | Native server | Yes | `go.etcd.io/bbolt`. Pure Go, CGO-free. B+ tree with ACID transactions. Single writer, multiple readers. |
| `IndexedDBStore` | WASM client | Yes | Wraps browser IndexedDB via `syscall/js`. Object store keyed by record ID. Clock-indexed for range queries. |

### 4.3 bbolt Bucket Layout (BboltStore)

```
/records/{id}        → JSON-serialised Record
/clock_index/{clock} → record ID        (for GetChangesSince range scans)
/meta/node_id        → this server's NodeID
/meta/knowledge_vec  → serialised KnowledgeVector
```

`Put()` writes the record and clock index entry in a single bbolt transaction. `GetChangesSince()` does a cursor seek on `/clock_index/` starting from the given HLC.

### 4.4 IndexedDB Layout (IndexedDBStore)

```
Database: nell-engine
  ObjectStore: records (keyPath: id)
    Index: clock (on record.clock.wall_time, non-unique)
  ObjectStore: outbox (keyPath: autoincrement)
    Stores {id, clock} of pending mutations
```

---

## 5. Ordering: Hybrid Logical Clocks

### 5.1 Why HLC

Clients can be offline for a week. Wall clocks drift. NTP is not guaranteed on mobile or Electron. Pure logical clocks (Lamport) solve ordering but produce timestamps unrelated to real time. Vector clocks capture causality but grow with the cluster.

HLC combines both: a physical wall time (for human-readable ordering and catch-up) with a logical counter (for causal ordering of events in the same millisecond). Two ints, 16 bytes. Converges to physical time when nodes communicate.

### 5.2 Algorithm

```
On local write:
  wall = max(local.WallTime, system.Now().UnixMilli())
  if wall == local.WallTime:
    local.Counter++
  else:
    local.Counter = 0
  local.WallTime = wall

On receiving a peer message with clock peerClock:
  local.WallTime = max(local.WallTime, peerClock.WallTime)
  local.Counter = max(local.Counter + 1, peerClock.Counter + 1)
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

### 6.2 Tombstones

A deletion sets `Deleted: true` with the current HLC. The tombstone propagates through sync like any other record. When a tombstone arrives with a clock higher than the local record, the local record is marked deleted. Tombstones are compacted after a configurable horizon (default: 30 days).

### 6.3 Conflict Callbacks (SDK)

The JS SDK exposes `onConflict(recordID, localVersion, acceptedVersion)` so applications can detect overwrites. The engine does not surface the losing version automatically (PoC scope).

---

## 7. Client Architecture (Offline-First)

### 7.1 Local Store

The WASM client runs `IndexedDBStore` (or `MemoryStore` for ephemeral mode). All reads and writes hit the local store first. There is no network round-trip for a write to succeed.

### 7.2 Outbox Log

Every mutation is tracked in an append-only outbox:

- On `Put()` or `Delete()`, append `{id, clock}` to the outbox.
- The outbox is ordered by HLC — the order mutations happened locally.
- Deduplication: only the latest version of each record ID is kept (previous outbox entries for the same ID are pruned on write).

### 7.3 Reconnection Sequence

When connectivity to the home server is restored:

```
1. PULL:  Client sends lastKnownServerClock → Server streams changes
2. APPLY: Each incoming record → Store.Put() (LWW resolves conflicts)
3. PUSH:  Client sends outbox entries → Server applies via its own Put()
4. ACK:   Server responds with confirmed clocks → Client clears outbox
5. WATCH: Client opens WebSocket for real-time sync
```

Pull-then-push ordering minimises the window for conflicts: the client sees the server's state before sending its own changes.

### 7.4 Offline Duration

No limit. The outbox grows linearly with the number of unique documents mutated while offline. Write-heavy offline workloads may produce thousands of outbox entries; deduplication keeps this bounded to one entry per document.

---

## 8. Server Architecture

### 8.1 Deployment Modes

| Mode | Description |
|---|---|
| **Standalone** | One server process. Accepts client syncs. No peer replication. |
| **Distributed mesh** | Multiple server processes. Peer-to-peer replication via gossip + anti-entropy. |

Same binary. Configured via a `peers` list or mDNS discovery.

### 8.2 Sync Protocol

#### Real-time Push (WebSocket)

- Servers maintain WebSocket connections to known peers.
- When a local write succeeds, the mutation is broadcast as JSON to all connected peers.
- WebSocket ping/pong frames detect liveness.

#### Anti-Entropy Pull (HTTP)

- Endpoint: `POST /sync/check`
- Request body: `{"sender_node_id": "...", "vector": {"node-a": "1749283920000:42", ...}}`
- Response body: `{"receiver_node_id": "...", "missing_changes": [{...}, ...]}`
- Algorithm:
  1. Server A sends its Knowledge Vector to Server B.
  2. Server B iterates its store for records where `Record.Clock > Vector[Record.UpdatedBy]`.
  3. Server B streams missing records back.
  4. Server A applies each via `Store.Put()` (LWW convergence).

#### Client Sync (HTTP + WebSocket)

- Endpoint: `POST /sync/pull` — client sends last-known server clock, receives changes.
- Endpoint: `POST /sync/push` — client sends outbox entries, receives acks.
- After push/pull, client upgrades to WebSocket for real-time.

### 8.3 Knowledge Vector

```go
type KnowledgeVector map[string]HLC
```

Each server tracks the highest HLC it has seen from every other node (identified by `UpdatedBy`). The vector is:
- Updated on every `Put()`: `vector[record.UpdatedBy] = max(vector[record.UpdatedBy], record.Clock)`.
- Persisted in bbolt under `/meta/knowledge_vec`.
- Exchanged during anti-entropy to compute deltas.

### 8.4 Peer Discovery

| Method | Scope | How |
|---|---|---|
| **mDNS** | LAN | Advertise `_nell-core._tcp` service. Browse for peers on same subnet. |
| **Seed peers** | WAN | Configured static list. Server connects to seeds, requests peer list. |

### 8.5 Peer State Machine

```
[Discovered] → ACTIVE (gossip enabled)
ACTIVE → DEGRADED (missed heartbeat, gossip suspended)
DEGRADED → ACTIVE (heartbeat recovered)
DEGRADED → DEAD (max retries exceeded, purged)
```

Heartbeat loop: background ticker pings each peer. Missed pings transition state. Gossip push is suspended to degraded/dead peers to prevent buffer bloat.

---

## 9. JS SDK Surface

### 9.1 NellDB Class

```js
class NellDB {
  async init(wasmUrl)           // Load WASM, start Go runtime
  async put(record)             // {id, type, payload, vector?} → {updated, record}
  async get(id)                 // → Record | null
  async delete(id)              // → {updated, record} (record has deleted:true)
  async list(filter?)           // → Record[]
  async searchSimilar(vector, limit) // → Record[] sorted by cosine similarity
  async sync(serverUrl)         // Start sync with home server
  async close()                 // Shut down

  // Lifecycle
  onConnect(callback)           // WebSocket established
  onDisconnect(callback)        // Connection lost
  onConflict(callback)          // (id, local, accepted) called on LWW overwrite
  onSyncComplete(callback)      // Pull+push cycle finished
}
```

### 9.2 Initialisation

```js
import { NellDB } from './nell.js';
const db = new NellDB();
await db.init('./nell.wasm');
await db.put({ id: 'note-1', type: 'text', payload: 'Hello' });
await db.sync('https://home.example.com');
```

### 9.3 WASM Bridge

`client/main.go` (build tag `js,wasm`) registers global JS callbacks:

```go
js.Global().Set("nellPut", js.FuncOf(func(this js.Value, args []js.Value) any {
    var rec core.Record
    json.Unmarshal([]byte(args[0].String()), &rec)
    updated, current := store.Put(rec)
    resp, _ := json.Marshal(map[string]any{"updated": updated, "record": current})
    return js.ValueOf(string(resp))
}))
```

`main()` blocks on `<-ch` to keep the WASM instance alive. The JS SDK calls `go.run(instance)` which registers all callbacks, then the class methods call those global functions.

---

## 10. Vector Search

### 10.1 Strategy

Linear scan with Cosine Similarity. Pure Go, no dependencies, compiles to WASM.

```go
func CosineSimilarity(a, b []float32) float32 {
    // dot product / (norm(a) * norm(b))
}
```

Acceptable for PoC scale (thousands of vectors). Swappable for HNSW later.

### 10.2 Store Interface Addition

```go
type Store interface {
    // ...
    SearchSimilar(vector []float32, limit int) ([]Record, error)
}
```

`MemoryStore` implementation: iterate all records with `Type == TypeVector`, compute cosine distance, return top-K sorted.

### 10.3 Future: HNSW-lite

The spec-sim swarm converged on a SIMD-friendly HNSW-lite index with quantised int8 vectors stored in a columnar SoA layout. This is scoped for post-PoC.

---

## 11. Compression

### 11.1 Per-Payload Compression

Payloads exceeding a configurable threshold (default: 1 KB) are compressed before storage using `compress/zlib` (stdlib). A 1-byte flag in the record header indicates compression.

### 11.2 Wire Compression

Sync batches over HTTP are gzip-compressed (`Content-Encoding: gzip`). WebSocket frames are sent uncompressed for latency (individual mutations are small).

---

## 12. Safety & Transactions

### 12.1 Local Write Atomicity

A `Put()` operation in `BboltStore` writes both the record and clock index entry in a single bbolt transaction. Either both land or neither does. The `MemoryStore` equivalent holds `sync.Mutex` for the duration of the write.

### 12.2 Crash Recovery

`BboltStore` uses bbolt's built-in crash recovery: write-ahead log with page-level atomicity. On restart, bbolt replays the WAL and recovers to the last consistent state.

`IndexedDBStore` relies on IndexedDB's transaction durability (browser-managed).

### 12.3 Multi-Document Batches (Future)

Not in PoC scope. The spec-sim swarm proposed a two-phase commit protocol with explicit intent logs. This is deferred.

---

## 13. Build System

### 13.1 `go:generate`

`client/generate.go`:

```go
//go:generate env GOOS=js GOARCH=wasm go build -ldflags="-s -w" -o nell.wasm main.go
//go:generate cp $GOROOT/misc/wasm/wasm_exec.js .
```

Running `go generate ./client/...` produces:
- `nell.wasm` — the compiled engine
- `wasm_exec.js` — Go's WASM runtime shim

Combined with the hand-written `nell.js`, this forms the complete client SDK bundle.

### 13.2 Makefile Targets

```makefile
build-wasm:   go generate ./client/...      # → client/nell.wasm + wasm_exec.js
build-server: go build -o bin/nell-server server/main.go
build-all:    build-wasm build-server
```

### 13.3 Distribution

The JS SDK ships as a single directory:
```
client/
├── nell.js          ← import this
├── nell.wasm        ← loaded at runtime
└── wasm_exec.js     ← Go runtime shim
```

The Go server is a library:
```go
import "github.com/samcharles93/nell-engine/server"
```

---

## 14. Project Directory Structure

```
nell-engine/
├── types.go             # package nell — Record, HLC, DataType
├── store.go             # package nell — Store interface, MemoryStore, ResolveConflict, KnowledgeVector
├── logstore/
│   └── log.go           # package logstore — append-only Zstd-compressed LogStore
├── client/
│   ├── main.go          # WASM entrypoint + syscall/js callbacks (build: js && wasm)
│   ├── generate.go      # go:generate directives
│   └── nell.js          # JS SDK wrapper
├── server/
│   ├── main.go          # HTTP server + WebSocket fan-out
│   └── replication.go   # PeerManager interface
├── sdk/
│   ├── doc.go           # package sdk — Doc, Change, DocRange types
│   ├── docdb.go         # DocDB — document API (MVCC _rev, real-time changes)
│   ├── replicate.go     # Replicator — Push/Pull/Sync/Live
│   ├── meta.go          # Persisted meta:clock for incremental pull
│   ├── vector.go        # Persisted meta:vector (KnowledgeVector on disk)
│   ├── rev.go           # _rev generation (gen-sha1 tokens)
│   └── changes.go       # Changes feed hub
├── cmd/nell-server/
│   └── main.go          # Server CLI entrypoint
├── examples/
│   └── example.go       # Runnable tour of the SDK
├── docs/
│   ├── status.md
│   ├── technical-design.md
│   └── architecture/adrs/
├── go.mod
└── Makefile
```

---

## 15. Implementation Phases

### Phase 1 — Core Engine (Week 1-2)

- [x] `types.go` — Record, HLC, DataType (done)
- [x] HLC tick logic, GreaterThan, Update, clock string
- [x] `store.go` — Store interface, MemoryStore with LWW conflict resolution (done)
- [ ] `vector.go` — CosineSimilarity, SearchSimilar on MemoryStore
- [x] `store_test.go` — 35 tests: HLC, LWW, KnowledgeVector, MemoryStore (edge/stress/concurrency)

### Phase 2 — Persistent Server Storage (Week 2-3)

- [x] `logstore/log.go` — LogStore with Zstd-compressed append-only binary log
- [x] `logstore/log_test.go` — 15 tests: persistence, replay, corruption, concurrency
- [ ] Bucket layout: records, clock_index, meta
- [ ] `GetChangesSince` via cursor walk on clock_index
- [ ] Crash recovery test

### Phase 3 — Server Sync (Week 3-4)

- [ ] `server/peer.go` — PeerManager, KnowledgeVector
- [ ] `server/replication.go` — anti-entropy logic, gossip broadcast
- [ ] `server/handler.go` — HTTP endpoints: /sync/check, /sync/pull, /sync/push
- [ ] WebSocket fan-out on mutation
- [ ] `server/discovery.go` — MeshRegistry, seed peers

### Phase 4 — WASM Client (Week 4-5)

- [ ] `client/main.go` — WASM callbacks for put, get, delete, list, search (partial done)
- [ ] `store/indexeddb.go` — IndexedDBStore via syscall/js (or MemoryStore for PoC)
- [ ] `client/nell.js` — NellDB class with full API surface
- [ ] Sync lifecycle hooks: onConnect, onDisconnect, onConflict, onSyncComplete

### Phase 5 — Integration & Polish (Week 5-6)

- [ ] End-to-end: WASM client → mock server → sync push/pull
- [ ] Outbox log on client
- [ ] Pull-then-push reconnection sequence
- [ ] Conflict callback in JS SDK
- [ ] `go generate` pipeline verified
- [ ] README with quickstart

### Phase 6 — Hardening (Post-PoC)

- [ ] mDNS discovery
- [ ] Peer state machine heartbeat loop
- [ ] Payload compression
- [ ] Tombstone compaction
- [ ] HNSW-lite vector index
- [ ] Binary wire format (CBOR or flat buffers)

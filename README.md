# NellDB

NellDB is a distributed, real-time, document-oriented database. JSON-native, HTTP-synced, embeddable as a Go library or a JavaScript client.

## What you get

- **Document store** — arbitrary JSON keyed by `_id`, with MVCC `_rev` for safe concurrent writes and `_deleted` tombstones.
- **Real-time** — a live changes feed streams every local and remote write as it happens. Subscribe and react.
- **Distributed** — any number of nodes sync over plain HTTP. A per-peer knowledge vector drives anti-entropy so concurrent writes from a new peer don't get dropped.
- **Embeddable** — the server is a Go library. The client is a JavaScript class. Import, embed, ship.
- **One codebase, three targets** — the same Go code compiles to a native server, a WASM client, and an in-process library. No drift.
- **Zero storage dependencies** — one transitive dep (Zstd, for compression). The engine, the log-structured store, the sync protocol, the SDK are all in this repo.

## Install

```bash
go get github.com/samcharles93/NellDB
```

```js
npm install @nelldb/sdk   // coming with the v0.2 JS release
```

## Quick start — Go

```go
package main

import (
    "context"
    "fmt"

    "github.com/samcharles93/NellDB"
    "github.com/samcharles93/NellDB/sdk"
)

func main() {
    db := sdk.New(nell.NewMemoryStore("client"), "client")

    rev, _ := db.Put(context.Background(), sdk.Doc{
        sdk.FieldID: "note:1",
        "title":    "Hello",
        "body":     "world",
    })
    fmt.Println("wrote note:1 at rev", rev)

    doc, _ := db.Get(context.Background(), "note:1")
    fmt.Println("read back:", doc)
}
```

For a persistent store, swap `nell.NewMemoryStore` for `logstore.OpenLog("data.nell")`.

## Quick start — JavaScript

```js
import { NellDB } from "@nelldb/sdk";

const db = new NellDB();
await db.init();

await db.put({ _id: "note:1", title: "Hello" });
const doc = await db.get("note:1");

db.changes().on("change", (c) => console.log("change:", c));
await db.replicate.to("https://home.example.com");
```

## How sync works

When a node starts, it holds a per-peer *knowledge vector* — a map of `node-id → highest clock seen`. A pull sends that vector to the peer; the peer returns every record the sender has not seen. Updates to the vector are persisted, so a restarted client resumes incremental pulls instead of re-fetching the world.

A push sends local records; the peer applies them with last-write-wins on the engine's hybrid logical clock. Concurrent local and remote clocks are merged before each write, so the cluster converges to a single deterministic order.

The wire format is JSON over HTTP. The SDK writes `_rev` into the document body so revision tokens travel with the doc, end-to-end.

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│  JavaScript SDK (nell.js)            ← @nelldb/sdk          │
│  NellDB class: put, get, query, delete, changes, replicate  │
├─────────────────────────────────────────────────────────────┤
│  WASM Client (client/main.go)        ← //go:build js && wasm │
│  Go → JS syscall/js bridge: nellPut, nellGet, nellList       │
├─────────────────────────────────────────────────────────────┤
│  Document API (sdk/)                 ← import ".../sdk"     │
│  DocDB, Doc, _rev MVCC, Replicator, Changes feed            │
├─────────────────────────────────────────────────────────────┤
│  HTTP Server (server/)               ← import ".../server"   │
│  /sync/pull  /sync/push  /sync/check   (no WebSocket yet)   │
├─────────────────────────────────────────────────────────────┤
│  Storage Backend                     ← import ".../logstore" │
│  LogStore: append-only Zstd-compressed frames, replay on    │
│  open.  Pluggable via the nell.Store interface.             │
├─────────────────────────────────────────────────────────────┤
│  Core Engine (root package "nell")   ← import "..."         │
│  Record, HLC, DataType, Store interface, MemoryStore, LWW   │
└─────────────────────────────────────────────────────────────┘
```

A single Go codebase. The native `nelldb-server` binary is `cmd/nelldb-server`. The WASM build is `client/`. Everything else is importable as a library.

## When to use NellDB

- You want a document DB without standing up a cluster.
- Your app needs to work offline and converge when reconnected.
- You need a real-time changes feed (think live cursors, multiplayer, sync indicators).
- You're building a single-binary Go service that also needs a JS/browser client, and you don't want to maintain two engines.

## When not to use it

- You need mature multi-region replication, sharding, and ops tooling. Use Firestore, DynamoDB, or a managed Postgres.
- You need ACID transactions across documents. NellDB is per-document atomic only.
- You need a SQL query layer. Use a SQL database.
- You need > 1 GB single-doc payloads. NellDB is for documents, not blobs; store those in object storage.

## Limitations (v0.1)

- No compaction — the persistent log grows unbounded. v0.2 will add a sweep.
- No WebSocket sync — `Replicator.Live` polls. v0.2 will add a push channel.
- No Mango-style queries — range scans via `AllDocs` only.
- No attachments — store binary fields as base64 inside a doc.
- LWW conflict resolution at the engine. `_rev` detects stale local writes; cross-node conflicts still resolve by HLC.

## Repo layout

```
NellDB/
├── types.go              package nell     ← core types, Store, MemoryStore, LWW
├── store.go              package nell
├── logstore/             package logstore ← persistent Zstd log
├── server/               package server   ← HTTP sync, anti-entropy
├── sdk/                  package sdk      ← DocDB, Replicator
├── client/               package client   ← WASM runtime + JS SDK
├── cmd/nelldb-server/      package main     ← standalone binary
├── examples/                              ← runnable tour
├── docs/                                  ← status, design, ADRs
└── scripts/                                ← build helpers
```

## Status

See [docs/status.md](./docs/status.md) for what's built and what's next. See [docs/technical-design.md](./docs/technical-design.md) for the deep dive. Architecture decisions live in [docs/architecture/adrs/](./docs/architecture/adrs/).

# NellDB

Distributed, real-time, document-oriented database. Native Go engine, WASM client, HTTP sync.

## Install

```bash
go get github.com/samcharles93/NellDB
```

## Library (Go)

```go
import (
    "github.com/samcharles93/NellDB"
    "github.com/samcharles93/NellDB/sdk"
)

db := sdk.New(nell.NewMemoryStore("node-1"), "node-1", "issues")

rev, _ := db.Put(ctx, sdk.Doc{"_id": "1", "status": "open"})
doc, _ := db.Get(ctx, "1")
```

## Storage

NellDB is storage-agnostic via the `nell.Store` interface.

- **In-memory**: `nell.NewMemoryStore(nodeID)` — ephemeral.
- **Durable**: `logstore.OpenLog(path, nodeID)` — append-only, Zstd-compressed frame log with parallel replay.

## Sync

Multi-primary replication over HTTP. Nodes use Hybrid Logical Clocks (HLC) for causal ordering and knowledge vectors for incremental delta-sync.

```go
rep := sdk.NewReplicator(db, "https://peer-node:9343")
go rep.Live(ctx, 30*time.Second) 
```

## Structure

- `logstore/`: Durable append-only Zstd log.
- `sdk/`: DocDB, MVCC, and Replicator.
- `server/`: HTTP API and anti-entropy handlers.
- `client/`: WASM runtime and JS bridge.
- `cmd/nelldb-server/`: Standalone server binary.

## Documentation

- [Technical Design](docs/technical-design.md)
- [Architecture Decisions (ADRs)](docs/architecture/adrs/)
- [Project Status](docs/status.md)

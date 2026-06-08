# Copilot instructions for `NellDB`

## Build, test, and validation

- Run the full Go test suite with `go test ./...`.
- Run a single test with the standard Go pattern, for example:
  - `go test ./sdk -run '^TestReplicatorRoundtrip$'`
  - `go test ./server -run '^TestServerPushThenPull$'`
- Run the WASM integration coverage with `make test-wasm`. That host-side test builds `client/main.go` for `js/wasm` and executes it under Node, so it exercises the real exported callbacks instead of a native substitute.
- Run static analysis with `go vet ./...`. The repo treats this as part of normal validation (`cmd/nell-server/build_test.go` checks it).
- Run lint-style checks with `staticcheck ./...`.
- Build the standalone server with `make build-server` or `go build -o bin/nell-server ./cmd/nell-server/`.
- Build the WASM bundle with `make build-wasm`.
- `go generate ./client/...` is also a real build path here: it produces `client/nell.wasm` and `client/wasm_exec.js`, and `cmd/nell-server/build_test.go` checks that the generate directives stay valid.

## High-level architecture

- The root `nell` package is the shared engine. `types.go` defines `HLC`, `Record`, and `DataType`; `store.go` defines the `Store` interface, `MemoryStore`, `KnowledgeVector`, and LWW conflict resolution.
- `logstore/` is the durable `Store` implementation. `logstore.OpenLog(path, nodeID)` replays an append-only Zstd-compressed frame log into an in-memory index on startup, and `Compact` rewrites the log to keep only current winners and retained tombstones.
- `sdk/` is the application-facing document layer. `DocDB` maps user `sdk.Doc` values onto engine `nell.Record` values, manages MVCC `_rev` tokens, exposes `AllDocs`/changes feeds, and persists replication metadata in the same underlying store.
- `server/` exposes the HTTP sync surface:
  - `/sync/push` ingests batches through engine LWW resolution
  - `/sync/pull` returns records newer than a single HLC
  - `/sync/check` is the anti-entropy endpoint driven by `KnowledgeVector`
- `sdk.Replicator` is the important replication path for Go clients. Its pull path uses `/sync/check`, not `/sync/pull`, so per-peer knowledge vectors survive concurrent writes from different nodes. `MeshManager` in `server/peer.go` uses the same endpoint for periodic server-to-server reconciliation.
- `client/` is the WASM + JS bridge. `client/main.go` exposes global JS callbacks (`nellPut`, `nellGet`, `nellDelete`, `nellList`) backed by a `MemoryStore`; `client/nell.js` is a thin wrapper around those callbacks. This side is simpler than the Go SDK and still has TODOs around full sync behavior.
- `cmd/nell-server/` is only the CLI wiring: flags choose `MemoryStore` vs `logstore`, then wrap it in `server.New(...)` and optionally start the peer mesh loop.
- `examples/example.go` is the fastest end-to-end orientation for the Go SDK: local CRUD, `_rev` conflicts, `AllDocs`, changes feeds, and replication against a running server.

## Key conventions

- Keep the storage boundary clean: anything above persistence should depend on `nell.Store`, not on `MemoryStore` or `LogStore` directly.
- There are two different concurrency/conflict layers:
  - Local stale-write detection is the SDK’s `_rev` check.
  - Cross-node convergence is engine-level LWW using `HLC` first and lexical `UpdatedBy` as the deterministic tie-break.
- In the SDK, `_rev` is stored inside the JSON payload that becomes `nell.Record.Payload`. The in-memory `revs` map is only a cache rebuilt from stored records.
- Reserved document fields are `_id`, `_rev`, and `_deleted`. Everything else in `sdk.Doc` is application data and should round-trip unchanged.
- Replication metadata is stored as ordinary records with synthetic IDs `meta:clock` and `meta:vector`. These must be filtered out of replication payloads and can appear if you inspect the raw store directly.
- If you change replication logic, preserve the knowledge-vector flow. `sdk.Replicator.Pull`, `sdk.DocDB.observeVector`, `sdk/meta.go`, `sdk/vector.go`, `server.handleCheck`, and `server.MeshManager` all participate in “what has this node already seen?”
- `List()` on the engine returns only non-deleted records, but tombstones are still meaningful for replication and compaction. Do not treat “not listed” as “never existed.”
- The changes feed is best-effort, not lossless: `changesHub` drops when subscriber buffers fill, and `DocDB.Changes` only drains what is still buffered on cancellation. Code that needs a complete view should reconcile with `AllDocs`/replication rather than assuming every event is delivered.
- Treat tests as effectively immutable. Only change a test when it is clearly wrong, and explain that reasoning explicitly; never rewrite tests just to make implementation failures disappear.
- Some repo docs are aspirational compared with the current implementation. For behavior and invariants, prefer the current code in `types.go`, `store.go`, `logstore/`, `sdk/`, `server/`, `client/`, and the tests over older design prose.

# Changelog

All notable changes for this project will be documented in this file.

## [v0.2.5] - 2026-06-19

### Fixed
- **Binary push broadcast**: `handleBinPush` was accepting records via the binary sync endpoint but never broadcasting them to WebSocket peers. JSON push (`handlePush`) broadcast fine; binary push silently dropped the broadcast step. This meant peers connected via WebSocket wouldn't see changes until their next poll cycle. Fixed: `handleBinPush` now collects accepted records and calls `s.broadcast()` the same way `handlePush` does. Also added metrics recording to match the JSON path.

### Added
- **JS SDK expansion**: the WASM client (`client/main.go`) and JS SDK (`client/nell.js`) now expose the full SDK API surface:
  - `putMany(docs[])` / `nellPutMany` — batch insert with rollback on error
  - `getMany(ids[])` / `nellGetMany` — batch fetch, missing IDs silently skipped
  - `changes(callback)` / `nellChanges` — real-time changes feed bridged from Go channel to JS callback, with `stopChanges(handle)` to cancel
  - `startSync(url, interval, secret)` / `nellLiveSync` — continuous HTTP polling sync with configurable interval and HMAC auth, returns a handle for `stopSync()`
  - `startSyncWS(url, secret)` / `nellLiveWS` — continuous WebSocket sync with auto-reconnect, returns a handle for `stopSync()`
  - `stopSync(handle)` / `nellStopSync` — stops a running sync loop and fires `onDisconnect`
  - `setAuth(secret)` / `nellSetAuth` — sets HMAC secret for all subsequent sync calls
  - `destroy()` / `nellDestroy` — tombstones all documents
- **Lifecycle hooks wired**: `onConnect` fires when `startSync`/`startSyncWS` establishes a connection; `onDisconnect` fires on `stopSync`. Previously these hooks were registered but never invoked.
- **WASM test**: `TestWASMBulkOps` exercises `putMany`, `getMany`, `changes` feed, and `destroy` under Node with fake-indexeddb.

## [v0.2.4] - 2026-06-19

### Changed
- **Collection index + HLC index for `MemoryStore`**: the in-memory store now maintains the same `map[collection]set[key]` and lazily-rebuilt `[]clockKey` indexes as the logstore, bringing `List`/`ListAll` and `GetChangesSince` to O(collection size) / O(log n + k) respectively. This aligns `MemoryStore` with `LogStore` so the server `--in-memory` mode, WASM client fallback, and SDK tests all get the same scan improvements. `IndexedDBStore` already used native `IDBKeyRange` queries and was already optimal. Measured in-memory range scan (10K rows from 1M): 521ms → 241ms (2.2x faster).

## [v0.2.3] - 2026-06-19

### Changed
- **Collection index for `List`/`ListAll`**: the logstore now maintains a `map[collection]set[key]` index, maintained incrementally on every write and rebuilt on replay/Compact. `List` and `ListAll` iterate only the keys in the requested collection instead of scanning the full records map, making them O(collection size) instead of O(total records). Measured 2.4-3.7x faster across 1K-100K records, with proportional memory allocation reduction (12.5 MB → 2.7 MB at 100K records spread across 5 collections).

## [v0.2.2] - 2026-06-19

### Added
- **`OpenLogWithOptions`**: configurable logstore open with `Options{FlushInterval, CompressionLevel}`. `OpenLog` remains the safe default (per-write flush, SpeedDefault zstd) — no silent behaviour change for existing callers.
- **Group commit**: `Options.FlushInterval > 0` enables a background flush goroutine. Writes return immediately after buffering; the kernel page cache is flushed on the interval. Trades up to `FlushInterval` of writes on a process crash for ~1.5x write throughput. Neither mode calls `fsync` — durability against power loss is a separate concern.
- **Compression level knob**: `Options.CompressionLevel` selects the Zstd encoder level (Fastest/Default/Better/Best). `SpeedFastest` roughly halves encode time vs `SpeedDefault` at a modest ratio cost.
- **HLC index for `GetChangesSince`**: the replication hot path is now O(log n + k) via a lazily-rebuilt sorted index instead of a full O(n) scan. Writes mark the index dirty (O(1)); the next query rebuilds it if needed. Measured 2.1x faster at 1K records, 2.8x at 10K, 3.6x at 100K — improvement grows with scale.
- **`storage` config section**: `flush_interval_ms` and `compression_level` in `nell.yaml` to control group-commit and zstd level from the server binary.
- **Benchmarks**: `BenchmarkPutLocal`, `BenchmarkPutSequentialSameID` to track write-path regressions.

### Changed
- `LogStore` struct gains `changesIdx`, `changesDirty`, `flushInterval`, `stopFlush` fields.
- `cmd/nelldb-server/main.go` uses `OpenLogWithOptions` with config-driven storage options.

## [v0.2.1] - 2026-06-19

### Changed
- **Faster compaction**: `LogStore.Compact` no longer rescans the entire log file. The in-memory `records` map already holds the post-LWW winner for every key (it is updated on every `Put`), so the file scan — which decompressed and unmarshalled every frame only to redo conflict resolution that the live index already reflects — was pure waste. Compaction now iterates `ls.records` directly, turning a Zstd-decompress-and-unmarshal O(n) scan into a plain map copy. The `readFrame` helper, previously only used by compaction, has been removed.

## [v0.1.11] - 2026-06-13

### Added
- **Multi-node replication**: MeshManager now reconciles with all active peers per tick (max 4 concurrent) instead of one random peer, reducing propagation delay in multi-node meshes.
- **Peer state machine**: `TrackedPeer` with Active/Degraded/Dead states and a background heartbeat loop (`HEAD /health` every 10s). Dead peers are excluded from reconciliation and broadcast.
- **mDNS peer discovery**: `server/discovery.go` advertises `_nell-core._tcp` via mDNS and auto-discovers peers on the local network. Enabled via `--discovery` flag or `discovery.enabled` in `nell.yaml`. Gracefully degrades on platforms without multicast (Docker, WSL).
- **WebSocket SDK client**: `Replicator.LiveWS(ctx, nodeID)` for real-time push-based sync with automatic reconnect and exponential-backoff jitter. Complements existing `Live()` HTTP polling.
- **HMAC auth in SDK**: `Replicator.SetAuthSecret()` signs all HTTP requests with `X-Nell-Timestamp` / `X-Nell-Signature` headers. Shared `nell.SignBody()` extracted to `sign.go`. WebSocket endpoint now protected by HMAC when auth secret is configured.

### Fixed
- **Tombstone propagation**: `handleCheck`/`handleBinCheck` now use `listAll()` (via `GetChangesSince`) instead of `List()` so deleted records propagate via anti-entropy. SDK `Push()` similarly uses `DocDB.listAll()` so client→server pushes include tombstones.
- **Goroutine leak in MeshManager.Stop()**: `Stop()` now closes `stopCh` to signal both the anti-entropy and heartbeat goroutines.

### Changed
- Example binaries moved to their own package directories (`examples/tour/`, `examples/perf/`, `examples/perf-persist/`, `examples/sync-bench/`) to fix "main redeclared" build errors.
- `TrackedPeer` replaces bare `[]string` URLs in MeshManager. Public API (`AddPeer`, `RemovePeer`, `Peers`) preserved.

## [v0.1.10] - 2026-06-13

### Changed
- Minimalist README rewrite focused on library usage and structure.
- Removed AI-generated "slop" and generic marketing language.

## [v0.1.9] - 2026-06-13

### Fixed
- Import syntax error in `logstore/log.go` causing build failures.
- Durable storage replay logic: moved discard check before frame append to correctly handle truncated files and avoid EOF errors on startup.

## [v0.1.8] - 2026-06-13

### Changed
- Initial feature-complete release of core engine, logstore, SDK, and HTTP sync.

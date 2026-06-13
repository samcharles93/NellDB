# Changelog

All notable changes to this project will be documented in this file.

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

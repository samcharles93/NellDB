# ADR 0011: Offline-First Client Architecture

- **Status:** Proposed
- **Date:** 2026-06-07

## Context

Clients (Obsidian plugins, Electron apps, Capacitor apps) must work fully offline for extended periods — days to weeks. Local reads and writes must always succeed regardless of network state. When connectivity returns, changes must sync automatically with minimal user-visible latency.

This is the fundamental requirement that distinguishes NellDB from cloud-only databases and drives the entire HLC, LWW, and multi-primary design.

Existing offline-first databases (e.g. PouchDB on top of CouchDB) handle this with local IndexedDB storage and a replication protocol. NellDB needs the same capability, but the client must run in environments where neither a Go runtime nor Node bindings are available — only the browser JS / WASM sandbox.

## Decision

### Local Store

The client runs the same `Store` interface as the server, backed by IndexedDB (browser) or a WASM-compatible store. All reads and writes hit the local store first — there is no network round-trip for a write to be considered "successful."

### Outbox Log

Every mutation is tracked in an append-only outbox:

- When a record is written locally, the mutation is appended to the outbox with its pre-sync HLC clock.
- The outbox is ordered by clock — the order mutations happened on this device.
- The outbox lives alongside the data store (IndexedDB).

### Reconnection Sequence (Pull-then-Push)

When connectivity to the home server is restored:

1. **Pull:** The client sends its last-known server clock to the server and receives all changes that happened while it was offline. Each incoming record is applied via `Store.Put()` — LWW conflict resolution handles any overlap with local changes.
2. **Push:** The client sends all outbox entries to the server in order. The server applies each via its own `Store.Put()` (LWW).
3. **Ack:** The server responds with confirmed clock values. The client clears acknowledged outbox entries.
4. **Resume realtime:** The client establishes a WebSocket connection for ongoing real-time sync.

### Conflict Semantics

- If a client edited a document offline and the server also received edits to the same document from another node, LWW determines the winner based on HLC clocks.
- The client SDK exposes an `onConflict(recordID, local, accepted)` callback so the application can react to overwrites.

## Consequences

### Positive

- Local reads and writes are instant — no network dependency.
- Full offline operation for any duration.
- Pull-then-push ordering minimises the window for conflicts (client sees server state before sending local changes).
- The outbox provides a clear audit trail of unsynchronised changes.

### Negative

- The outbox grows during offline periods. For heavy writers this could be thousands of entries (mitigation: single entry per record ID in the outbox — only the final state matters).
- Pull-then-push means reconnection time is proportional to the duration of disconnection (more changes to pull and push).
- LWW means local changes may be silently discarded if a server-side clock is higher.

### Mitigations

- Deduplicate the outbox by record ID before pushing (keep only the latest version of each record).
- Show sync progress in the SDK (`onSyncProgress(pulled, pushed, total)`).
- The conflict callback gives applications an escape hatch from silent LWW.

---

**Not yet implemented.** The current `MemoryStore` in WASM is ephemeral — IndexedDB persistence and the outbox log are the next client milestones.

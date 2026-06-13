# ADR 0009: P2P Multi-Primary Replication Protocol

- **Status:** Proposed
- **Date:** 2026-06-07

## Context

Nell-engine servers must replicate data between each other without a single leader or central coordinator. Every server accepts writes from its local clients and from peer servers. The system must converge to the same state across all nodes.

This is the "CouchDB without a central CouchDB" problem — each NellDB instance is both the "CouchDB server" and a peer in a mesh, and there is no cluster-wide consensus.

Requirements:
- A write made to any server must propagate to all other servers.
- A server that was offline or partitioned must catch up when it reconnects.
- Real-time propagation for online nodes; efficient reconciliation for offline ones.
- No single point of failure — any server can serve reads/writes independently.

## Decision

Implement a two-pronged replication strategy: **real-time gossip push** for online peers and **background anti-entropy pull** for reconciliation.

### Real-Time Gossip (Push)

- Servers maintain long-lived WebSocket connections to known peers.
- When a local write succeeds, the mutation is serialised as JSON and broadcast to all connected peers immediately.
- The WebSocket connection is also used for peer liveness detection (heartbeat/ping frames).
- Push is suspended to peers in a non-Active state (see ADR 0010).

### Anti-Entropy (Pull)

- Every server exposes an HTTP POST endpoint: `/api/v1/sync/check`.
- A background goroutine periodically (every `N` seconds, configurable) initiates anti-entropy with randomly selected peers.
- The request carries the server's `KnowledgeVector` (see ADR 0008).
- The response carries records the requesting server is missing.
- The requesting server feeds each record through `Store.Put()` — LWW conflict resolution ensures convergence.

### Wire Protocol

```
── Real-time push ──────────────────────►
WebSocket: {"type": "mutation", "record": {...}}

── Anti-entropy pull ──────────────────►
POST /api/v1/sync/check
{ "sender_node_id": "server-A", "vector": {"server-B": "1749283920000:42"} }

◀── Anti-entropy response ────────────────
{ "receiver_node_id": "server-A", "missing_changes": [{...}, ...] }
```

### Interface

```go
type PeerManager interface {
    BroadcastMutation(rec core.Record)
    ReconcileWithPeer(peerURL string) error
    GetLocalKnowledgeVector() core.KnowledgeVector
}
```

## Consequences

### Positive

- Real-time propagation for online nodes (sub-second latency).
- Anti-entropy heals partitions automatically — even after extended disconnection.
- No single leader, no Raft, no quorum.
- Delta-only sync — only missing records are exchanged.

### Negative

- Real-time push can fail silently if the WebSocket drops without notification (heartbeat mitigates this).
- Anti-entropy frequency must be tuned — too frequent burns CPU/bandwidth, too infrequent leaves stale data.
- The `PeerManager` interface does not specify WebSocket reconnection semantics — the implementation must handle back-pressure, reconnection back-off, and connection pooling.

### Mitigations

- Use WebSocket ping/pong frames for liveness detection on the push path.
- Exponential back-off for anti-entropy retries.
- Make anti-entropy interval configurable per peer.

---

**Skeleton:** `server/replication.go`

# ADR 0008: Knowledge Vector for Replication State

- **Status:** Accepted
- **Date:** 2026-06-07

## Context

For multi-primary replication, each server needs to know what the other servers have and haven't seen. Without this, every anti-entropy sync would require scanning the entire database — untenable at any scale.

CouchDB uses a `_changes` feed with sequence numbers. In a multi-primary system without a single source of truth, there is no single sequence number. Each node produces its own stream of changes, and each peer must track where it is in every other node's stream.

## Decision

Represent replication state as a **Knowledge Vector** — a map from node ID to the highest HLC clock consumed from that node:

```go
type KnowledgeVector map[string]HLC
```

- The key is the `UpdatedBy` value of a node (its `NodeID`).
- The value is the highest HLC clock the local node has seen from that peer.
- When a record arrives, the Knowledge Vector is updated:
  `vector[record.UpdatedBy] = max(vector[record.UpdatedBy], record.Clock)`.

The vector is serialised and exchanged between peers during anti-entropy sync:

```go
type SyncStateRequest struct {
    SenderNodeID string          `json:"sender_node_id"`
    Vector       KnowledgeVector `json:"vector"`
}

type SyncStateResponse struct {
    ReceiverNodeID string   `json:"receiver_node_id"`
    MissingChanges []Record `json:"missing_changes"`
}
```

The anti-entropy algorithm:

1. Server A sends its `KnowledgeVector` to Server B.
2. Server B iterates its store and finds records where `Record.Clock > Vector[Record.UpdatedBy]`.
3. Server B streams those records back.
4. Server A applies them through `Store.Put()` (LWW conflict resolution).
5. Both servers update their vectors.

The Knowledge Vector is persisted as part of the server's metadata so it survives restarts.

## Consequences

### Positive

- Compact state representation: for N nodes, the vector has at most N entries.
- Efficient anti-entropy: only records newer than the peer's last-seen clock are sent.
- The vector serves double duty: it's both the sync state tracker and the conflict resolution context.

### Negative

- The vector grows linearly with the number of unique writing nodes. Each client + server is a node.
- Does not detect conflict branches (intentional for PoC — LWW doesn't need them anyway).

### Mitigations

- Clients that only read do not need entries in the vector (only writing nodes are tracked).
- Periodic compaction can prune stale entries for nodes that haven't been heard from in weeks.
- For the PoC, the vector stays in memory and is rebuilt from the store on restart.

---

**Skeleton:** `server/replication.go` — `PeerManager` interface references `KnowledgeVector`.

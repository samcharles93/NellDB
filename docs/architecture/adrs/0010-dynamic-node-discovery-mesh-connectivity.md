# ADR 0010: Dynamic Node Discovery and Mesh Connectivity

- **Status:** Proposed
- **Date:** 2026-06-07

## Context

For a distributed P2P mesh, servers need to find each other. Hardcoding peer addresses doesn't scale — IPs change, new nodes join, existing nodes leave. At the same time, we reject heavy service registries (Consul, etcd, Kubernetes DNS) as they violate the "self-hosted, no infra dependencies" ethos.

Servers may be on the same LAN (home network) or connected over WAN (Tailscale, VPN, public IPs). Discovery must work in both cases.

## Decision

Use a two-tier discovery model: **mDNS for LAN** and **seed peers for WAN**, managed by a **Peer State Machine**.

### LAN Discovery (mDNS)

- On startup, each server advertises its `_nell-core._tcp` service type via multicast DNS.
- The advertisement includes its `NodeID`, API port, and protocol version.
- A background mDNS browser discovers other `_nell-core._tcp` services on the same subnet.
- Discovered peers are added to the mesh registry automatically.

### WAN Discovery (Seed Peers)

- The server accepts a configuration array `seed_peers: ["192.168.1.50:8080", "..."]`.
- On startup, the server connects to each seed and requests its peer list.
- Seeds gossip known peers back, forming an overlay network.
- Suitable for Tailscale, VPN, or static IP deployments.

### Peer State Machine

Each peer transitions through three states:

```
     [Discovered]
          │
          ▼
    ┌──────────┐   heartbeat fails   ┌───────────┐   max retries   ┌──────┐
    │  ACTIVE  │ ──────────────────► │ DEGRADED  │ ──────────────► │ DEAD │
    │ (gossip  │ ◄────────────────── │ (gossip   │                │      │
    │  enabled)│   heartbeat recovers│  suspended│                │      │
    └──────────┘                     └───────────┘                └──────┘
```

- **Active:** Peer responds to heartbeats. Real-time gossip push is enabled.
- **Degraded:** Peer missed >=1 heartbeat but < max retries. Gossip is suspended to avoid memory pile-up in network buffers. Anti-entropy pull still works.
- **Dead:** Peer exceeded max retries or didn't respond for a prolonged period. Removed from the active mesh. Its Knowledge Vector entry is preserved so reconnection can use anti-entropy efficiently.

```go
type PeerState string

const (
    StateActive   PeerState = "active"
    StateDegraded PeerState = "degraded"
    StateDead     PeerState = "dead"
)

type Peer struct {
    NodeID      string
    Address     string
    State       PeerState
    LastSeen    time.Time
    MissedPings int
}
```

A background heartbeat loop ticks every `N` seconds and transitions peers through the state machine. The interval and max failures are configurable.

## Consequences

### Positive

- Zero-configuration LAN operation — start two servers on the same network and they find each other.
- WAN operation via static seed list works for self-hosted deployments over VPN/Tailscale.
- The state machine prevents memory bloat by suspending gossip to dead peers.
- Knowledge Vectors are preserved for dead peers, so reconnection uses efficient delta sync.

### Negative

- mDNS does not traverse subnets or WAN boundaries — seed entries are required for multi-location deployments.
- The heartbeat loop generates background network traffic (minimal — single-byte ping or TCP dial).
- mDNS libraries in pure Go exist but may have edge cases on unusual network configurations.

### Mitigations

- Make seed peer list configurable and optional (LAN-only deployments omit it).
- Heartbeat can use the existing WebSocket connection rather than a separate ping.
- Consider `hashicorp/memberlist` (SWIM gossip protocol, pure Go) as the membership backend if mDNS proves insufficient.

---

**Not yet implemented.** This ADR guides the implementation of `server/discovery.go`.

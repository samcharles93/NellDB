# NellDB Deployment Guide

## Quickstart — single node

Start an ephemeral node with no persistence, no auth, no peers:

```bash
nelldb-server --in-memory --addr :9342
```

The server listens on `:9342` (the default port), logs structured JSON to
stderr, and answers health probes at `/health`.

To write and read data, use the HTTP sync API or run the issue-tracker client
pointed at this node (see [Client setup](#client-setup) below).

## Two-node mesh

Start two nodes that replicate to each other. Both use the same `--auth-secret`
so their sync requests are authenticated.

**Node A (home-server):**

```bash
nelldb-server \
  --node-id home-server \
  --data /var/lib/nell/nell.db \
  --addr :9342 \
  --peers http://laptop.local:9342 \
  --auth-secret "changeme-32-bytes-minimum!"
```

**Node B (laptop):**

```bash
nelldb-server \
  --node-id laptop \
  --data ./nell.db \
  --addr :9342 \
  --peers http://home-server.local:9342 \
  --auth-secret "changeme-32-bytes-minimum!"
```

What happens:

- Every 30 seconds each node runs anti-entropy against every active peer via
  `/sync/check` (knowledge-vector diff).
- Every 10 seconds each node sends a `HEAD /health` heartbeat to every peer to
  track liveness.
- A peer is marked **degraded** after 1 missed heartbeat and **dead** after
  3 consecutive misses. Dead peers are skipped during anti-entropy.

Pass `--discovery` to enable mDNS — nodes on the same LAN find each other
automatically and `--peers` is not required on every node.

## Config file

All flags can be expressed in a `nell.yaml` file. The server loads it with
`--config nell.yaml` (default: `nell.yaml` in the working directory).

Env var substitution works via `${VAR_NAME}` or `$VAR_NAME` in any value.

```yaml
server:
  port: 9342
  data_dir: /var/lib/nell
  max_skew_ms: 500          # max clock drift for HMAC timestamps (ms)
  tls_cert: /etc/nell/cert.pem
  tls_key: /etc/nell/key.pem

web:
  enabled: false             # serve the Web UI at /ui/

auth:
  secret: ${NELL_AUTH_SECRET}
  # JWKS alternative (mutually exclusive with secret):
  # jwks_url: https://example.com/.well-known/jwks.json
  # jwks_refresh_interval_seconds: 3600

sync:
  max_batch_size: 1000
  staleness_eviction_days: 14

discovery:
  enabled: false             # mDNS LAN peer discovery

compaction:
  interval_minutes: 60       # how often to compact the log
  tombstone_ttl_hours: 168   # 7 days (see Compaction section)

peers:
  - http://laptop.local:9342
  - http://cloud-vm.example.com:9342

vector:
  enable_hnsw: true
  pca_dimensions: 128
  training_sample_size: 5000
  retraining_insert_threshold: 50000
  pq_subspaces: 16
  pq_centroids: 256
```

**Important:** The config file path is `nell.yaml` by default. Flags override
config file values.

## TLS setup

NellDB supports TLS for all HTTP traffic (sync, health, WebSocket). Generate a
self-signed certificate with OpenSSL:

```bash
openssl req -x509 -newkey rsa:4096 -keyout key.pem -out cert.pem \
  -days 365 -nodes \
  -subj "/CN=nell.example.com" \
  -addext "subjectAltName=DNS:nell.example.com,DNS:localhost,IP:192.168.1.10"
```

Pass the files via flags:

```bash
nelldb-server \
  --tls-cert /etc/nell/cert.pem \
  --tls-key /etc/nell/key.pem \
  --addr :9342
```

Or via `nell.yaml`:

```yaml
server:
  port: 9342
  tls_cert: /etc/nell/cert.pem
  tls_key: /etc/nell/key.pem
```

When both `--tls-cert` and `--tls-key` are present, the server calls
`ListenAndServeTLS` instead of `ListenAndServe`. If only one is present the
server starts without TLS and logs a warning — the missing file is silently
ignored.

**Client-side:** When a replicator connects to a TLS server, point it at the
`https://` URL. For self-signed certs the client must either trust the CA or
configure a custom `http.Transport` with `InsecureSkipVerify`.

## Compaction

The append-only log grows forever unless compacted. Compaction rewrites the
log, keeping only the latest version of each record and dropping tombstones
older than a configurable TTL.

**Defaults:**

| Field              | Default | Meaning                                    |
|--------------------|---------|--------------------------------------------|
| `interval_minutes` | 60      | Run compaction every 60 minutes            |
| `tombstone_ttl_hours` | 168  | Drop tombstones older than 7 days (168 h)  |

**Tuning:**

- **Low-write deployments:** Increase `interval_minutes` to 360 (6 hours) to
  reduce I/O.
- **High-churn deployments:** Decrease `tombstone_ttl_hours` to 24 so deleted
  records are reaped faster.
- **One-shot compact:** The `Compact(tombstoneThreshold)` method can be called
  programmatically. Pass a zero duration to drop **all** tombstones immediately.

**How it works:**

1. Scans the entire log, keeping only the highest-HLC frame per record ID.
2. Filters tombstones whose clock is older than the TTL cutoff.
3. Writes surviving records to a temp file with Zstd compression.
4. Atomically renames the temp file over the original.
5. Reopens the file and rebuilds the in-memory index.

The store remains usable during compaction. If compaction fails the original
log is preserved and the temp file is cleaned up.

## Health endpoint

**`GET /health`** — liveness probe. Returns 200 as long as the process is
running and the HTTP server is accepting connections.

```json
{"status": "ok", "node_id": "home-server"}
```

**`GET /ready`** — readiness probe. Verifies the store is operable by listing
one record.

```json
{"status": "ready", "node_id": "home-server", "doc_count": 42}
```

Both are unauthenticated — they are served outside the HMAC auth wrapper so
load balancers and orchestrators can poll them directly.

**Polling:**

```bash
# Liveness
curl -s http://localhost:9342/health | jq .

# Readiness
curl -s http://localhost:9342/ready | jq .
```

**Replication lag:** NellDB does not expose a `replication_lag_seconds` metric
on the health endpoint directly. Lag can be inferred by comparing the
`KnowledgeVector` across peers:

- Every node maintains a per-peer map of the highest clock it has seen from
  that peer.
- The anti-entropy loop runs every 30 seconds, so in practice a healthy mesh
  converges within 30–60 seconds of a write.
- To measure lag programmatically, call `/sync/check` on a peer with an empty
  KnowledgeVector and examine the timestamp of the most recent record returned.

**Kubernetes probes example:**

```yaml
livenessProbe:
  httpGet:
    path: /health
    port: 9342
  initialDelaySeconds: 3
  periodSeconds: 10

readinessProbe:
  httpGet:
    path: /ready
    port: 9342
  initialDelaySeconds: 5
  periodSeconds: 15
```

## Client setup

The issue-tracker binary (and any SDK-based client) connects to a NellDB
server for replication using two flags:

| Flag            | Env / Config equivalent | Purpose                       |
|-----------------|-------------------------|-------------------------------|
| `--sync-url`    | `sync.remote_url`       | Base URL of the NellDB server |
| `--sync-secret` | `sync.secret`           | HMAC shared secret            |

When a client runs with `--sync-url`, it creates a `Replicator` that:

1. **Pushes** all local records to the server via `/sync/bin/push` (binary
   protocol).
2. **Pulls** all remote records it hasn't seen via `/sync/bin/check`.
3. **Repeats** both on a 30-second timer (`Live` loop).

The client stores a meta-clock in its local store so after a restart it resumes
incrementally rather than re-fetching the full dataset.

The same mechanism works bidirectionally: multiple issue-tracker instances can
each connect to the same or different NellDB servers, and the mesh propagates
changes everywhere.

**Client with HMAC auth:**

```bash
issue-tracker \
  --sync-url http://cloud-vm.example.com:9342 \
  --sync-secret "${NELL_SYNC_SECRET}"
```

**Client via config file (`issue-tracker.yaml`):**

```yaml
sync:
  enabled: true
  remote_url: http://cloud-vm.example.com:9342
  secret: "${NELL_SYNC_SECRET}"
  interval: 30s
```

## Example: Spawn deployment

This example shows how to deploy NellDB for the Spawn use case: a homelab
server, a laptop, and a public cloud VM that all keep the same set of issues
in sync.

**Networking:**

- All three machines are on a Tailscale network (tailnet).
- The cloud VM has TLS enabled because it's reachable over the public internet
  (even though Tailscale encrypts the wire, TLS adds a layer of defense for
  the sync secret).
- The homelab server and laptop communicate over Tailscale's WireGuard network
  (already encrypted and authenticated).
- All nodes share the same HMAC secret.

**Config for the homelab server (`/etc/nell/nell.yaml`):**

```yaml
server:
  port: 9342
  data_dir: /var/lib/nell

auth:
  secret: ${NELL_AUTH_SECRET}

compaction:
  interval_minutes: 120
  tombstone_ttl_hours: 72

peers:
  - http://laptop.tailnet.ts.net:9342
  - https://nell.example.com:9342
```

Start:

```bash
export NELL_AUTH_SECRET="$(cat /etc/nell/auth-secret.txt)"
nelldb-server --config /etc/nell/nell.yaml
```

**Config for the laptop (`~/nell.yaml`):**

```yaml
server:
  port: 9342
  data_dir: ~/.nell

auth:
  secret: ${NELL_AUTH_SECRET}

peers:
  - http://home-server.tailnet.ts.net:9342
  - https://nell.example.com:9342
```

Start:

```bash
export NELL_AUTH_SECRET="$(cat ~/.nell/auth-secret.txt)"
nelldb-server --config ~/nell.yaml
```

**Config for the cloud VM (`/etc/nell/nell.yaml`):**

```yaml
server:
  port: 9342
  data_dir: /var/lib/nell
  tls_cert: /etc/letsencrypt/live/nell.example.com/fullchain.pem
  tls_key: /etc/letsencrypt/live/nell.example.com/privkey.pem

auth:
  secret: ${NELL_AUTH_SECRET}

peers:
  - http://home-server.tailnet.ts.net:9342
  - http://laptop.tailnet.ts.net:9342
```

Start:

```bash
export NELL_AUTH_SECRET="$(cat /etc/nell/auth-secret.txt)"
nelldb-server --config /etc/nell/nell.yaml
```

**issue-tracker instance on the laptop (`issue-tracker.yaml`):**

```yaml
sync:
  enabled: true
  remote_url: http://home-server.tailnet.ts.net:9342
  secret: "${NELL_SYNC_SECRET}"
  interval: 15s

spawner:
  enabled: true
  max_concurrency: 3
  mcp_url: http://home-server.tailnet.ts.net:9343/mcp
```

Start:

```bash
export NELL_SYNC_SECRET="$(cat ~/.nell/auth-secret.txt)"
issue-tracker --config issue-tracker.yaml
```

**What this achieves:**

- Writes from any issue-tracker instance arrive at the local NellDB server
  first, then propagate to peers via anti-entropy (every 30s) and real-time
  WebSocket broadcast.
- The laptop and homelab server talk over Tailscale (encrypted, no extra TLS
  needed). The cloud VM talks to both over TLS.
- If the cloud VM goes down, the laptop and homelab server continue syncing
  between themselves. When the VM returns, anti-entropy catches it up.
- The shared HMAC secret ensures only authenticated nodes can push data into
  the mesh.
- Compaction runs on each node independently every 2 hours, dropping
  tombstones after 72 hours.

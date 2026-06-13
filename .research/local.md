# Local code recon — conflict-handling / reliability pass

Read-only survey of `/work/apps/nell-engine`. No files were edited.
Four issues mapped, plus test inventory and a "what the local code implies" close.

---

## A) Issue 2 — Zero tests for IndexedDBStore

### A.1 `indexeddb_wasm.go` (root, build tag `js && wasm`)

File: `/work/apps/nell-engine/indexeddb_wasm.go` (full file, 285 lines).

**Build tag and package** (lines 1-3):
```go
//go:build js && wasm
package nell
```
Because the build tag is `js && wasm`, `go test ./...` (host build) **never compiles
this file**, so it has effectively zero compile-time coverage from the existing suite.

**IDBObjectStore / index / keypath** (lines 41-58 in `NewIndexedDBStore`):
```go
request := jsIndexedDB.Call("open", "NellDB", 1)
...
// Create object store "records" with keyPath: "id"
objectStore := db.Call("createObjectStore", "records", map[string]any{
    "keyPath": "id",
})

// Create non-unique index "clock" on clock.wall_time for range queries.
// Record struct uses json:"clock" and HLC uses json:"wall_time".
objectStore.Call("createIndex", "clock", "clock.wall_time", map[string]any{
    "unique": false,
})
```
- One DB: `"NellDB"`, version `1`.
- One object store: `"records"`, keyPath `"id"`.
- One index: `"clock"` on path `"clock.wall_time"` (uses HLC's JSON tag, `types.go:25`).
- No `multiEntry`, no compound key, no `autoIncrement`.

**Transactions / `getAll` / cursor**:
- `Put` (lines 86-139): `readwrite` txn over `["records"]`, then `store.Call("put", jsObj)`. Calls `s.clock.Update(incoming.Clock)` first and then runs `ResolveConflict` *before* the IDB write, so IDB is the durable mirror, the engine logic is in Go.
- `Get` (lines 141-184): `readonly` txn + `store.Call("get", id)`. Missing -> `ErrRecordNotFound`.
- `Delete` (lines 186-200): reads via `Get`, sets `Deleted = true`, calls `Put` (which still goes through LWW).
- `List` (lines 202-242): `readonly` txn + `store.Call("getAll")` — uses `getAll`, no cursor. **Filters `Deleted` in Go** (line 235), not in the query.
- `GetChangesSince` (lines 244-307): uses the `clock` index with an `IDBKeyRange.lowerBound` and an `openCursor` (not `getAll`).

**`GetChangesSince` IDBKeyRange shape** (lines 247-256):
```go
store := txn.Call("objectStore", "records")
index := store.Call("index", "clock")

keyRange := js.Global().Get("IDBKeyRange").Call("lowerBound", since.WallTime, false)
request := index.Call("openCursor", keyRange)
```
- Lower bound is `since.WallTime` only — **not a compound `[wallTime, counter]`**. The HLC `counter` is ignored by IDB, so the JS range filter returns every record with `wall_time >= since.WallTime` and the Go code then post-filters with `rec.Clock.GreaterThan(since)` (line 290). With `unique:false` index, multiple records sharing the same wall_time are allowed and `continue()` walks all of them.
- `lowerBound` second arg is `false` (exclusive). For an exact equality, this is "strictly greater than `since.WallTime`", so a record whose `wall_time == since.WallTime` but `counter > since.Counter` is still included by the cursor and is kept or dropped by the post-filter.

**What requires real IDB behavior**:
1. `transaction()` over a list of stores with the right mode.
2. `createObjectStore` + `createIndex` inside `onupgradeneeded`. The first open in a fresh DB must trigger the upgrade handler; subsequent opens must not.
3. `onupgradeneeded`, `onsuccess`, `onerror` async event ordering. The current code uses a `done` channel per request and `defer Release()` on each `js.Func`. These channels are only safe if the callbacks really do run (Node won't fire them without a polyfill).
4. `IDBKeyRange.lowerBound` on a non-unique index.
5. `openCursor` with `continue()` semantics and the `null` sentinel for "no more rows" (line 268).
6. `getAll` returning a JS array with `length` and integer index (line 226, 229).
7. `JSON.parse` / `JSON.stringify` round-tripping the HLC `Clock` so the index path `"clock.wall_time"` resolves. The HLC is `omitempty`-less, so `Clock: HLC{}` and `Clock: HLC{WallTime:0,Counter:0}` both serialize, but the comment at line 49 documents the dependency.

### A.2 WASM test harness

File: `/work/apps/nell-engine/client/wasm_test.go` (172 lines). The harness string is the `const nodeHarness` at the bottom of the file.

**Test entry point** (lines 11-29):
```go
func TestWASMCallbacks(t *testing.T) {
    if _, err := exec.LookPath("node"); err != nil {
        t.Skip("node not in PATH; skipping WASM integration test")
    }

    root := repoRoot(t)
    wasmPath := buildWASMClient(t, root)
    scriptPath := writeNodeHarness(t)
    wasmExecPath := wasmExecPath(t)

    cmd := exec.Command("node", scriptPath, wasmExecPath, wasmPath)
    cmd.Dir = root
    out, err := cmd.CombinedOutput()
    if err != nil {
        t.Fatalf("run WASM harness:\n%s", out)
    }
}
```

**`go test` flags** (none beyond the run pattern). The make target drives:
```
go test ./client -run '^TestWASM'
```
(See Makefile below.)

**`buildWASMClient`** (lines 31-46): runs `go build -o <tmp>/nell-test.wasm ./client/main.go` with `GOOS=js GOARCH=wasm CGO_ENABLED=0`. Output goes to `t.TempDir()`, never `client/nell.wasm` on disk.

**`writeNodeHarness`** (lines 48-56): writes the embedded `nodeHarness` const to a temp `.js` file and `node`s it.

**The `nodeHarness` string** (lines 90-172) — this is the natural injection point:
```js
const fs = require("fs");
const path = require("path");
const util = require("util");
const crypto = require("crypto");
const { performance } = require("perf_hooks");

async function main() {
  const [, , wasmExecPath, wasmPath] = process.argv;
  ...
  globalThis.require = require;
  globalThis.fs = fs;
  globalThis.path = path;
  globalThis.TextEncoder = util.TextEncoder;
  globalThis.TextDecoder = util.TextDecoder;
  globalThis.performance ??= performance;
  globalThis.crypto ??= crypto.webcrypto ?? crypto;

  require(wasmExecPath);

  const go = new Go();
  go.argv = [wasmPath];
  go.env = { TMPDIR: require("os").tmpdir() };
  ...
```
**There is no `require("fake-indexeddb/auto")` line.** The natural fix is to add
`require("fake-indexeddb/auto");` after the existing `globalThis.*` polyfills and
before `require(wasmExecPath)`. Node's `require` already exists, so the auto-shim
will populate `globalThis.indexedDB` and `globalThis.IDBKeyRange` and the existing
`NewIndexedDBStore` will run unmodified. The harness also needs the dependency
available; the typical pattern is to install `fake-indexeddb` and `require.resolve`
it, or vendor a tiny local shim.

**Makefile target** (`/work/apps/nell-engine/Makefile` lines 13-15):
```make
test-wasm:
	@echo "Running WASM integration tests..."
	go test ./client -run '^TestWASM'
```
The `-run` filter matches the regex `^TestWASM` so the test name can stay
`TestWASMCallbacks` or be renamed to `TestWASMIndexedDB*` to scope fake-indexeddb
runs.

### A.3 MemoryStore substitution in tests — confirmed

File: `/work/apps/nell-engine/client/main.go` lines 138-147:
```go
func main() {
    ch := make(chan struct{}, 0)

    var err error
    store, err = nell.NewIndexedDBStore("wasm-client")
    if err != nil {
        fmt.Println("Falling back to MemoryStore:", err)
        store = nell.NewMemoryStore("wasm-client")
    }
    db = sdk.New(store, "wasm-client")
```
Under Node today, `jsIndexedDB.IsUndefined()` is `true`, so `NewIndexedDBStore`
returns `"indexedDB is not available in this environment"` (line 26 of
`indexeddb_wasm.go`) and we silently fall through to `MemoryStore`. The fallback
is a `fmt.Println` only, so `TestWASMCallbacks` passes without exercising any
IDB code path. This is exactly the gap the user is asking about.

The test harness's CRUD round-trip exercises `sdk.DocDB` against `MemoryStore`:
- `wasm_test.go:128-138` does `put` → `get` → `allDocs` → `remove` → `allDocs` (empty) → `put(invalid JSON)` → `get("")`. None of it touches IDB.
- `wasm_test.go:7` (the `package` declaration) puts the test in `package client`, not `package main`, so it cannot directly import the build-tagged `indexeddb_wasm.go`; the only way to drive that code is via the WASM bundle.

### A.4 `cmd/nelldb-server/build_test.go` already builds the WASM bundle

For reference, `TestWASMBuild` (lines 37-50) is the build-only path; it does
*not* execute the binary. It confirms `client/main.go` compiles for
`GOOS=js GOARCH=wasm` and exits non-zero on failure.

---

## B) Issue 4 — Replicator Live is a 5s polling loop; server already broadcasts via /sync/ws

### B.1 `sdk.Replicator` and the Live polling loop

All in one file: `/work/apps/nell-engine/sdk/replicate.go`.

- Struct (lines 24-28):
  ```go
  type Replicator struct {
      DB      *DocDB
      BaseURL string
      HTTP    *http.Client
  }
  ```
- Constructor (lines 33-39): `NewReplicator(db, baseURL)`. Default `http.Client` with 30s timeout.
- `Pull` (lines 48-99): posts `{sender_node_id, vector}` to `/sync/check`, decodes `missing_changes`, calls `r.DB.ingestRemote(rec)` for each, then `r.DB.advanceClock(maxSeen)`. **Not** a single-clock `since` against `/sync/pull`.
- `Push` (lines 101-153): lists local records, filters `meta:*` (line 120-126), posts `{"changes": filtered}` to `/sync/push`, then `advanceClock` for the highest clock we sent.
- `Sync` (lines 156-159): push then pull.
- `LiveConfig` (lines 162-166):
  ```go
  type LiveConfig struct {
      Interval   time.Duration // pull cadence (default 5s)
      PushEvery  int           // push every N pulls (default 1 = every pull cycle)
      BackoffMax time.Duration // cap on backoff between failed pulls (default 1m)
  }
  ```
- `Live(ctx, cfg)` (lines 176-216) — the polling loop:
  ```go
  loopCtx, cancel := context.WithCancel(ctx)
  done := make(chan struct{})
  backoff := cfg.Interval

  go func() {
      defer close(done)
      pullCount := 0
      t := time.NewTimer(cfg.Interval)
      defer t.Stop()
      for {
          select {
          case <-loopCtx.Done():
              return
          case <-t.C:
              if pullCount%cfg.PushEvery == 0 {
                  if _, err := r.Push(loopCtx); err != nil {
                      backoff = nextBackoff(backoff, cfg.BackoffMax)
                      t.Reset(backoff)
                      continue
                  }
              }
              if _, err := r.Pull(loopCtx); err != nil {
                  backoff = nextBackoff(backoff, cfg.BackoffMax)
                  t.Reset(backoff)
                  continue
              }
              pullCount++
              backoff = cfg.Interval
              t.Reset(backoff)
          }
      }
  }()
  return func() {
      cancel()
      <-done
  }
  ```
  Default `Interval = 5 * time.Second` (line 178). The only start hook is
  calling `Live(...)`; the only stop hook is the returned closure. There is no
  `pause`, no per-`DocID` subscription, and no event integration with the
  WebSocket path.

- `nextBackoff` (lines 219-225): doubles up to `BackoffMax`. Reset is by reassignment on the success branch (line 211).

- `ingestRemote` (lines 247-275) — the path that consumes `Pull` results:
  ```go
  func (d *DocDB) ingestRemote(rec nell.Record) error {
      if isInternalID(rec.ID) {
          return nil
      }
      if _, _, err := d.store.Put(rec); err != nil {
          return err
      }
      rev, ok := readRev(rec)
      if !ok {
          rev = "1-remote"
      }
      d.mu.Lock()
      d.revs[rec.ID] = rev
      d.mu.Unlock()
      d.observeVector(rec.UpdatedBy, rec.Clock)

      doc := joinDoc(rec.ID, rec.Payload)
      if rec.Deleted {
          doc[FieldDeleted] = true
      }
      d.subs.broadcast(Change{ID: rec.ID, Rev: rev, Deleted: rec.Deleted, Doc: doc})
      return nil
  }
  ```
  This is the only entry point for remote changes today. There is no
  WebSocket path; nothing calls into `ingestRemote` outside `Replicator.Pull` and
  `TestChangesFeedIncludesRemote` (`sdk/docdb_test.go:271-321`).

### B.2 Server-side WS handler

File: `/work/apps/nell-engine/server/main.go`.

- WebSocket route (line 81):
  ```go
  mux.HandleFunc("/sync/ws", s.handleWebSocket)
  ```
- `upgrader` (lines 17-19): accepts all origins (`CheckOrigin: ... return true`). Important for browser WASM clients.
- `Server` struct (lines 26-34): holds `peers map[string]*peerConn` keyed by `r.RemoteAddr` (line 286), not by nodeID.
- `peerConn` (lines 268-271):
  ```go
  type peerConn struct {
      nodeID string
      mu     sync.Mutex
      conn   *websocket.Conn
  }
  ```
- `handleWebSocket` (lines 275-329): upgrade, read `node_id` query param (line 280, defaults to `"unknown"`), register in `s.peers` keyed by `RemoteAddr`, run a `ReadJSON` loop. **Inbound only** — the server reads from the socket, applies each received change via `s.store.Put`, then re-broadcasts. There is no initial push, no per-connection filter, and no last-seen clock per peer.
- `broadcast(changes)` (lines 343-356):
  ```go
  func (s *Server) broadcast(changes []nell.Record) {
      s.mu.RLock()
      defer s.mu.RUnlock()
      for _, p := range s.peers {
          go func(p *peerConn) {
              p.mu.Lock()
              defer p.mu.Unlock()
              if err := p.conn.WriteJSON(map[string]any{"changes": changes}); err != nil {
                  slog.Error("websocket broadcast failed", "peer", p.nodeID, "err", err)
              } else {
                  slog.Info("[broadcast] records", "peer", p.nodeID, "count", len(changes))
              }
          }(p)
      }
  }
  ```
  Message envelope is a flat JSON object: `{"changes": [Record, ...]}`. No
  envelope version, no `type` field, no per-record `clock` ack.

- Tracked client state: only `s.peers[remoteAddr] = &peerConn{nodeID, conn, mu}`. There is no subscription set, no per-client last-seen clock, no per-client `last-pushed-id`, no per-client `accept-rate`. After process restart, all clients reconnect and the server has no resume state for them.

- Auth: `server.HMACAuth` (`server/auth.go`) is middleware applied at the HTTP `mux` level in `cmd/nelldb-server/main.go:104` (`if len(authSecret) > 0 { h = server.HMACAuth(authSecret)(h) }`). **The WebSocket route is also under that middleware** because it lives in the same `mux`. The middleware reads headers `X-Nell-Timestamp` and `X-Nell-Signature` from the upgrade request, which is fine for browser WS clients that *can* set custom headers (the `WebSocket` JS API does not, so the client would need a different auth strategy or a non-HMAC scheme for the browser path). This is a real design constraint for issue B.

### B.3 JS bridge (`client/main.go` and `client/nell.js`)

`client/main.go` globals (registered at `registerCallbacks`, lines 17-117):
- `nellPut` (lines 20-33): `js.FuncOf` taking a JSON-encoded `sdk.Doc` string, returns `{ok, rev}` JSON.
- `nellGet` (lines 36-46): id string, returns `{ok, doc}` JSON.
- `nellRemove` (lines 49-73): id-or-doc, returns `{ok, rev}` JSON.
- `nellAllDocs` (lines 76-92): optional `sdk.DocRange` JSON, returns `{ok, result}` JSON.
- `nellSync` (lines 95-115): server URL, returns a `Promise<{ok, pushed, pulled}>` by calling `rep.Sync(ctx)` once in a goroutine.
- `nellReady` (line 116): `true`, used by the test harness as a "main has registered" signal.

There is **no** `nellConnect`, `nellSubscribe`, `nellOnChange`, `nellWSOpen` or
any WebSocket accessor. JS cannot subscribe to the changes feed directly
because the only bridge to it is `db.Changes(ctx)` and the only `ctx` is the
package-level `ctx` in `client/main.go:14`. There is no `nellChanges` global.

`client/nell.js` does not use `WebSocket` either:
- `class NellDB` (lines 18-138).
- Public methods: `init`, `put`, `get`, `remove`, `allDocs`, `sync`. `sync` is the one-shot `nellSync` wrapper (line 95-104). The promise resolves once and the connection is gone.
- Lifecycle hooks (lines 116-129): `onConnect`, `onDisconnect`, `onConflict`, `onSyncComplete` — all of them just stash the callback into `this._onX`. `onConnect`/`onDisconnect` are never invoked (there is no long-lived connection). `onConflict` is never invoked (see Issue C). `onSyncComplete` is invoked once after `sync` resolves (line 102).
- The `NellDB` class imports nothing, never touches `WebSocket`, and exposes no `connect`/`subscribe` method.

The `wasm_test.go` harness polls `globalThis.nellReady` for up to 2s (line 113) and then drives four CRUD calls. It does not exercise sync (no `nellSync` invocation) and there is no `WebSocket` server in the harness.

### B.4 `cmd/nelldb-server` wiring

`/work/apps/nell-engine/cmd/nelldb-server/main.go`:
- Flags (lines 13-23): `--addr`, `--node-id` (default `defaultNodeID()` = `os.Hostname()` or `"nell-server"`), `--data`, `--in-memory`, `--peers`, `--cert`, `--key`, `--auth-key`, `--metrics-addr`, `--rate-limit`, `--rate-burst`.
- `MeshManager` setup (lines 91-97): `NewMeshManager(srv, peers, 30*time.Second, authSecret)`, started only if `len(peers) > 0`. **MeshManager runs server-to-server `/sync/check` polling on a 30s ticker**, not WebSockets. The server-to-server mesh still polls.
- The `Server` already exposes `/sync/ws` regardless of the mesh config, but **nothing in the binary opens an outbound WS to a peer** — only the broadcast fan-out on inbound. The `MeshManager` interface (`server/peer.go:19-24`) has `BroadcastMutation` (lines 149-151) which is wired to `pm.srv.broadcast` but the mesh loop in `MeshManager.Start()` only does anti-entropy via `reconcileOne`, not mutation broadcast. So today the WS fan-out is only triggered by `/sync/push` and inbound WS messages (`server/main.go:189` `s.broadcast(req.Changes)` and `server/main.go:325` `s.broadcast(req.Changes)`).

### B.5 `sdk.DocDB.Changes` — exists but isn't reached

`/work/apps/nell-engine/sdk/db_changes.go` returns a `<-chan Change` (lines 14-67). The change is `sdk.Change{ID, Rev, Deleted, Doc}`. The channel is buffered to 64 and drops on backpressure (per `sdk/changes.go:34-40`). It is reachable from Go and is exercised by `TestChangesFeedIncludesRemote` (`sdk/docdb_test.go:271-321`). It is **not** reachable from the WASM/JS bridge because there is no `nellChanges` global and `nell.js` doesn't expose a corresponding method.

---

## C) Issue 5 — `_rev` is theater; silent LWW at the engine

### C.1 `_rev` generation, storage, and local Put

File `/work/apps/nell-engine/sdk/rev.go` (lines 14-30):
```go
func genRev(prev string, body []byte) string {
    gen := 1
    if prev != "" {
        parts := strings.SplitN(prev, "-", 2)
        if g, err := strconv.Atoi(parts[0]); err == nil && g > 0 {
            gen = g + 1
        }
    }
    sum := sha1.Sum(body)
    return strconv.Itoa(gen) + "-" + hex.EncodeToString(sum[:])
}
```
A `gen-sha1` content-hash rev. The `body` is `splitDoc(doc)` which is the user
Doc minus `_id` and `_deleted` but **including** `_rev` from the previous call
(see `splitDoc` at `sdk/docdb.go:573-583`). So the rev is a hash of the body
*after* `_rev` is stripped, then the new rev is stamped back into the body.

File `/work/apps/nell-engine/sdk/docdb.go` — the local `Put`:
- Reads current rev from in-memory cache (lines 168-170).
- Stale-write check (lines 172-178):
  ```go
  incomingRev, _ := doc[FieldRev].(string)
  if incomingRev != "" && curRev != "" && incomingRev != curRev {
      return "", ErrConflict
  }
  if incomingRev != "" && !exists {
      return "", ErrConflict
  }
  ```
- If `incomingRev == ""` (most read-modify-write loops), the chain is continued from the current local rev (line 183-186): `baseRev = curRev`. This is the "force write" behavior the docs call out.
- New rev is generated (line 191), the doc body is re-marshalled (line 195) and a `nell.Record` is constructed with `UpdatedBy = d.nodeID` and a fresh `HLC.Tick()` (lines 201-216). The HLC is merged with the existing record's clock so local writes don't accidentally drop behind (lines 209-213).
- Engine `store.Put` runs LWW (line 218). On the local path the local clock always wins because `clk.Tick()` strictly advances over the existing record.

### C.2 The Pull path: `ingestRemote` does NOT check `_rev`

File `/work/apps/nell-engine/sdk/replicate.go:238-275`:
```go
func (d *DocDB) ingestRemote(rec nell.Record) error {
    if isInternalID(rec.ID) {
        return nil
    }
    if _, _, err := d.store.Put(rec); err != nil {
        return err
    }
    rev, ok := readRev(rec)
    if !ok {
        rev = "1-remote"
    }
    d.mu.Lock()
    d.revs[rec.ID] = rev
    d.mu.Unlock()
    d.observeVector(rec.UpdatedBy, rec.Clock)
    ...
    d.subs.broadcast(Change{ID: rec.ID, Rev: rev, Deleted: rec.Deleted, Doc: doc})
    return nil
}
```
The losing record's `Payload` (and therefore its `_rev`) is thrown away by
`store.Put` (the engine returns the winner; the loser is not surfaced). The
caller (`Replicator.Pull`) discards the second return value:

```go
for _, rec := range out.MissingChanges {
    if err := r.DB.ingestRemote(rec); err != nil {
        return 0, fmt.Errorf("replicate pull ingest %q: %w", rec.ID, err)
    }
    ...
}
```
**No 3-way merge is possible from this path**: the loser is never delivered
to the SDK, so the SDK never has the prior rev to merge against.

### C.3 The `onConflict` callback — wired in JS only, never invoked

- JS setter: `client/nell.js:130`
  ```js
  onConflict(cb) { this._onConflict = cb; }
  ```
  Stashes the callback on the instance; nothing ever reads `this._onConflict`.
- Go SDK: `grep onConflict sdk/` returns zero hits. There is no `OnConflict`
  hook on `DocDB` or `Replicator` and no field for one on the struct
  (`sdk/docdb.go:42-58` shows `store`, `nodeID`, `mu`, `revs`, `vector`,
  `lastSeenClock`, `subs` — no `onConflict`).
- The user's `docs/status.md:101` already records this: "**Conflict callbacks
  in SDK** — `onConflict` hook exists but never fires. LWW silently overwrites.
  Should surface conflicts so apps can react."
- The only signal of a conflict the SDK has is `ErrConflict` from local
  `Put`/`Remove` (`sdk/docdb.go:24-29` sentinel). The remote path can't
  return that sentinel.

### C.4 Changes feed — does it carry the previous rev?

- Type: `sdk.Change{ID, Rev, Deleted, Doc}` (`sdk/doc.go:78-83`).
- `Rev` is the *new* rev (read from the payload in `ingestRemote` at
  `replicate.go:255-260`). The payload is the *winning* record. The previous
  rev is in the discarded losing payload and never reaches the SDK.
- The local `Put` path also only emits the new rev (line 226).
- A 3-way merge needs `old_rev, new_rev, current_rev`. `Change` carries only
  the new rev (which becomes `current_rev` after `ingestRemote`). The losing
  `old_rev` is lost at the engine. To do a 3-way merge, the SDK would need
  `Store.Put` to return both the winner and the loser, or a separate
  "previous" hook. Currently it returns `(accepted bool, current Record, err error)`
  (`store.go:21`). The bool is "did the incoming record win?", which is not
  the same as "what did it overwrite?".

### C.5 Engine-level LWW confirmation

File `/work/apps/nell-engine/store.go:45-58`:
```go
func ResolveConflict(local, incoming *Record) *Record {
    if incoming.Clock.GreaterThan(local.Clock) {
        return incoming
    }
    if local.Clock.GreaterThan(incoming.Clock) {
        return local
    }
    // Clocks equal → deterministic lexical tie-break on node ID
    if incoming.UpdatedBy > local.UpdatedBy {
        return incoming
    }
    return local
}
```
HLC primary, lexical `UpdatedBy` tie-break. Both `MemoryStore.Put`
(`store.go:108-128`) and `LogStore.Put` (`logstore/log.go:138-156`) call
`ResolveConflict`. `IndexedDBStore.Put` (`indexeddb_wasm.go:88-101`) also
calls it explicitly, so all three backends converge.

`HLC` definition and ordering (`types.go:30-78`): `WallTime` is unix
milliseconds, `Counter` is per-millisecond. `GreaterThan` is a total order on
`(WallTime, Counter)`. The equality branch falls through to the lexical
tie-break.

### C.6 Tie-break on `"wasm-client"` (the user's hint)

- `wasm-client` first appears in `client/main.go:141`:
  ```go
  store, err = nell.NewIndexedDBStore("wasm-client")
  ```
  Same string is then hardcoded again at line 144 (fallback `MemoryStore`) and
  line 146 (`sdk.New(store, "wasm-client")`). It is a Go literal; no JS reads
  or writes it. Since `wasm-client` is the only `UpdatedBy` on this client,
  every record this WASM produces has the same `UpdatedBy`. When two records
  collide with identical HLCs, `wasm-client` is compared against the peer's
  `UpdatedBy` (e.g. `"alice-server"`). Lexical tie-break: `alice-server` >
  `wasm-client`, so the peer's record wins. The user is right that this is
  a constant deterministic loss for the WASM node on HLC ties.
- Same string lives in the older plan doc
  `docs/superpowers/plans/2026-06-08-document-semantics-wasm.md:35, 36` —
  not authoritative code but evidence the value has been stable for a while.

---

## D) Issue 6 — Hardcoded `wasm-client` NodeID

### D.1 Every match for the literal `"wasm-client"`

From `grep -rn wasm-client`:
- `client/main.go:141`:
  ```go
  store, err = nell.NewIndexedDBStore("wasm-client")
  ```
- `client/main.go:144`:
  ```go
  store = nell.NewMemoryStore("wasm-client")
  ```
- `client/main.go:146`:
  ```go
  db = sdk.New(store, "wasm-client")
  ```
- `docs/superpowers/plans/2026-06-08-document-semantics-wasm.md:35` and `:36`
  are the historical plan copy, not code.

So the NodeID is set in **one place that fans out to three call sites**, all
in `client/main.go:141-146`. None of `client/nell.js`, the test harness, or
any other Go file sets it.

### D.2 `NodeID` (the accessor) in the repo

From `grep -rn NodeID` (case-sensitive):
- `store.go:78-79`: `MemoryStore.NodeID()` getter.
- `logstore/log.go:121-122`: `LogStore.NodeID()` getter.
- `sdk/docdb.go:111-112`: `DocDB.NodeID()` getter.
- `sdk/docdb.go:426` / `:444`: `Info.NodeID` field and population in `Info()`.
- `server/main.go:193`: `SenderNodeID` field on the `/sync/check` request body, not a NodeID accessor.

There is no `IndexedDBStore.NodeID()` method in `indexeddb_wasm.go` even
though the struct stores `nodeID string` (line 14). For consistency with
`MemoryStore` and `LogStore`, that getter is missing.

### D.3 Where the WASM client picks its node ID

It is a Go constant. `client/main.go:141` is in `main()`, runs once at
module boot. There is no `js.Global().Get("nellNodeID")` round-trip, no
`localStorage.getItem` read, no IDB read. The string `"wasm-client"` is
hardcoded into the binary. See also B.3 — JS never reads or writes the
node ID.

### D.4 Persistence options on the JS side

There is currently no persistence layer reachable from JS in the bundle.
`client/nell.js` and `client/main.go` have no `localStorage` or
`sessionStorage` usage (grep returns nothing). `IndexedDB` is the only
storage API used, and only via `nell.NewIndexedDBStore` (and only when IDB
is real, e.g. in the browser; under Node, the constructor errors out).

Two practical persistence strategies for a stable UUID:

1. **IndexedDB (in WASM)** — add a small key/value record at a fixed ID
   (e.g. `meta:node-id`) inside the existing `NellDB` IDB database. Read it
   in `NewIndexedDBStore` (or a separate `nodeID()` helper) and write a
   freshly generated UUID on first boot. Reuses the same `transaction` and
   `put` machinery already in `indexeddb_wasm.go:108-138`. Drawbacks: depends
   on the IDB upgrade handler having run, and the UUID write has to be
   `UpdatedBy = "wasm-client"` (or the resolved value), which is a chicken-
   and-egg situation; the cleanest pattern is to seed a `meta:node-id`
   record before the engine stamps any user records.

2. **localStorage (in JS)** — call `localStorage.getItem("nell:node-id")`
   from `client/nell.js`'s `init()` (or from a Go helper via
   `js.Global().Get("localStorage")`) and pass the value into a
   `NewIndexedDBStore(nodeID)` argument. Simpler — no transaction gymnastics
   — but couples the NodeID to the browser origin's storage. Survives page
   reload; cleared on user data wipe.

A third option (`crypto.randomUUID` then write the ID via a `js.FuncOf` that
calls back into Go) is more code than option 1 for the same durability.

---

## Test inventory

All `*_test.go` files in the repo:

- `/work/apps/nell-engine/store_test.go` (root):
  - `TestMemoryStorePutGet`
  - `TestMemoryStoreDelete`
  - `TestMemoryStoreLWW`
  - `TestMemoryStoreGetChangesSince`
  - `TestHLCClock`
- `/work/apps/nell-engine/offensive_test.go` (root):
  - `TestMemoryStoreConcurrentPuts`
  - `TestMemoryStoreConcurrentPutAndList`
  - `TestMemoryStoreConcurrentDeleteAndGet`
  - `TestMemoryStorePutEmptyID`
  - `TestMemoryStoreGetNonExistent`
  - `TestMemoryStoreDeleteNonExistent`
  - `TestMemoryStoreListEmpty`
  - `TestMemoryStoreGetChangesSinceFutureClock`
  - `TestMemoryStoreGetChangesSinceZeroClock`
  - `TestMemoryStorePutWithFutureClock`
  - `TestHLCMonotonic`
  - `TestHLCUpdateBackwardsClock`
  - `TestHLCEqualGreaterThan`
  - `TestHLCUpdateSameWallHigherCounter`
  - `TestHLCUpdateSameWallLowerCounter`
  - `TestHLCUpdateLaterWallTime`
  - `TestHLCString`
  - `TestKnowledgeVectorUpdateMonotonic`
  - `TestKnowledgeVectorMultipleNodes`
  - `TestLWWIdenticalClocks`
  - `TestLWWTombstoneWinsOverLiveWithLowerClock`
  - `TestLWWLiveWinsOverTombstoneWithLowerClock`
  - `TestLWWTieBreakLocal`
  - `TestLWWDeterministicAcrossOrder`
  - `TestKnowledgeVectorEmpty`
  - `TestKnowledgeVectorUpdateNewerCounterSameWall`
  - `TestKnowledgeVectorCopyIndependence`
  - `TestMemoryStoreManyRecords`
  - `TestMemoryStoreLargePayload`
  - `TestMemoryStoreGetChangesSinceEmpty`
- `/work/apps/nell-engine/sdk/docdb_test.go`:
  - `TestPutGetRoundtrip`
  - `TestPutConflict`
  - `TestGetMissing`
  - `TestRevMonotonicAndContentHash`
  - `TestRevIdenticalBodyStillIncrements`
  - `TestRemoveTombstone`
  - `TestRemoveWithRevConflict`
  - `TestPutManyAllOrNothing`
  - `TestGetMany`
  - `TestAllDocsRange`
  - `TestAllDocsByKeys`
  - `TestChangesFeed`
  - `TestChangesFeedIncludesRemote`
  - `TestReplicatorRoundtrip`
  - `TestReplicatorIdempotentPull`
  - `TestLastSeenClockPersistsAcrossRestart`
  - `TestReplicatorLivePicksUpNewRecords`
- `/work/apps/nell-engine/server/server_test.go`:
  - `TestServerPushThenPull`
  - `TestServerPushInvalidJSON`
  - `TestServerPushEmptyBody`
  - `TestServerPushEmptyChanges`
  - `TestServerPullInvalidJSON`
  - `TestServerCheckWithEmptyVector`
  - `TestServerWrongMethod`
  - `TestServerRejectsOversizedBody`
  - `TestHMACAuthAccept`
  - `TestHMACAuthReject`
  - `TestHMACAuthNoopWhenEmpty`
  - `TestServerConcurrentPushes`
  - `TestServerPushLargePayload`
  - `TestServerCheckReturnsMissing`
  - `TestServerCheckPartialKnowledge`
  - `TestServerCheckStaleClock`
  - `TestServerCheckServerReturnsOwnRecords`
  - `TestServerCheckTombstoneIncluded`
  - `TestMeshManagerReconcile`
  - `TestMeshManagerReconcileIdempotent`
  - `TestMeshManagerAddRemovePeers`
  - `TestMeshManagerStartStop`
- `/work/apps/nell-engine/logstore/log_test.go`:
  - `TestLogStorePersistence`
  - `TestLogStoreDeleteAndReopen`
  - `TestLogStoreCompactBasic`
  - `TestLogStoreCompactTombstoneRetention`
  - `TestLogStoreCompactDropsOldTombstones`
  - `TestLogStoreCompactKeepsLatestVersion`
  - `TestLogStoreCompactEmptyLog`
  - `TestLogStoreCompactReplay`
  - `TestLogStoreLWWOnReplay`
- `/work/apps/nell-engine/logstore/offensive_test.go`:
  - `TestLogStoreEmptyFile`
  - `TestLogStoreTruncatedFrame`
  - `TestLogStoreCorruptHeader`
  - `TestLogStoreZeroLengthFrames`
  - `TestLogStoreManyRecords`
  - `TestLogStoreManyOverwrites`
  - `TestLogStoreDoubleClose`
  - `TestLogStoreWriteAfterClose`
  - `TestLogStoreKnowledgeVectorSurvivesRestart`
  - `TestLogStoreConcurrentPuts`
  - `TestLogStoreConcurrentReadWrite`
  - `TestLogStoreConcurrentPutAndGetChangesSince`
- `/work/apps/nell-engine/client/wasm_test.go`:
  - `TestWASMCallbacks` — only test. Drives Node, no fake-indexeddb, no sync.
- `/work/apps/nell-engine/cmd/nelldb-server/build_test.go`:
  - `TestCGOIsDisabled`
  - `TestWASMBuild`
  - `TestGoModTidy`
  - `TestGoVet`
  - `TestNoCGOImports`
  - `TestGoGenerateValid`
  - `TestWASMRuntimeShimExists`
  - `TestDependencyCount`

Notes:
- `TestWASMBuild` (build only) and `TestWASMCallbacks` (run) are the only
  coverage of the WASM bundle. Neither exercises `IndexedDBStore`.
- `server/server_test.go` covers the HTTP sync endpoints, the MeshManager
  anti-entropy loop, and HMAC auth. It does not cover the WebSocket handler
  (`handleWebSocket`, `broadcast`, `peerConn`).
- No test imports `client/main.go` or `indexeddb_wasm.go`; both are
  `js && wasm`-tagged and unreachable from host tests.

### `make test-wasm` target

`/work/apps/nell-engine/Makefile` lines 13-15:
```make
test-wasm:
	@echo "Running WASM integration tests..."
	go test ./client -run '^TestWASM'
```
That's it — no `make` build dependency, no Node setup, no shim install. The
test itself builds the WASM binary to a temp dir and runs the embedded Node
harness. Adding `fake-indexeddb` requires either installing it as a module
and using `require.resolve` to load it, or shipping a vendored copy in the
repo (e.g. `client/fake-indexeddb-shim.js`) and adjusting the harness.

---

## What the local code implies

The four issues collapse into one design point: the engine and the
SDK/Replicator layer disagree about who owns conflict detection, and the
WASM client never had a way to express it. The engine owns LWW on HLC +
`UpdatedBy` tie-break (`nell/store.go:45-58`), and the SDK's `ingestRemote`
(`sdk/replicate.go:247-275`) throws the loser away; `_rev` is only
meaningful between two local `Put`s on the same node
(`sdk/docdb.go:172-178`). The changes feed type `sdk.Change{ID, Rev, Deleted, Doc}`
(`sdk/doc.go:78-83`) carries the new rev only, so even if the SDK wanted to
do a 3-way merge it has no prior rev to merge against. For issue C, the
minimum work is to make `nell.Store.Put` return both winner and loser (or
a callback), then have `ingestRemote` build a richer `Change` that
includes `PreviousRev`/`PreviousPayload`; for issue B, the server already
has the WS fan-out (`server/main.go:81`, `broadcast` at `:343-356`,
`peerConn` at `:268-271`) but neither the WASM bridge (`client/main.go`)
nor the JS SDK (`client/nell.js`) opens a socket — adding
`nellConnect(serverUrl)` and `nellSubscribe(cb)` globals plus a
`NellDB.connect()`/`db.onChange(cb)` is the natural shape, and that
becomes the same pipe that replaces `Replicator.Live`'s 5s poll
(`sdk/replicate.go:176-216`). For issue A, the only place to inject
`fake-indexeddb/auto` is the `nodeHarness` const at the bottom of
`client/wasm_test.go:90-172`; the fallback to `MemoryStore` at
`client/main.go:142-145` currently hides the lack of IDB coverage. For
issue D, the NodeID is a single Go literal in `client/main.go:141-146`
that needs to be replaced with a per-install UUID, and the only durable
storage reachable from the WASM main today is IndexedDB (or localStorage
from JS) — `client/nell.js` and `client/main.go` have no
`localStorage`/`sessionStorage` access at all. Two surprises worth
flagging: (1) `IndexedDBStore` is missing a `NodeID()` getter that
`MemoryStore` and `LogStore` both have, which is a small consistency bug
to fix while you're there; (2) the WebSocket path inherits HMAC auth from
the HTTP mux, but browser `WebSocket` cannot set custom headers, so a
client-side WS subscribe needs either a query-param secret, a subprotocol
header, or a separate auth-free `/sync/ws` route for browsers — this
constrains how the live push from B is wired.

Exact files to touch:

- `indexeddb_wasm.go` — add `NodeID()`, write a `meta:node-id` IDB record on first open.
- `client/main.go` (lines 138-147) — derive `nodeID` from a helper, register `nellConnect` and `nellSubscribe` callbacks, plumb a long-lived context for `db.Changes`.
- `client/nell.js` — add `connect(url)`, `onChange(cb)`, real `WebSocket` usage, invoke `onConflict` when `_rev` is dropped on a remote push.
- `client/wasm_test.go` (`nodeHarness` const, ~line 90) — `require("fake-indexeddb/auto")`; rename or add `TestWASMIndexedDB*` for the IDB code path.
- `Makefile` — `test-wasm` is the right place to wire any shim install step.
- `sdk/replicate.go` — `ingestRemote` (`:247-275`) is the conflict-detection choke point; `Live` (`:176-216`) becomes a thin WS wrapper.
- `sdk/docdb.go` — `Put`/Changes broadcast paths, plus the `Change` struct in `sdk/doc.go:78-83` needs a `PreviousRev`/`PreviousDoc` field for 3-way merge.
- `store.go` — `Store.Put` signature should grow a way to surface the loser (callback or second return).
- `server/main.go` — `handleWebSocket` (`:275-329`) needs an initial replay and per-peer resume state, plus an auth strategy that works for browser WS; `broadcast` (`:343-356`) needs a stable envelope version.

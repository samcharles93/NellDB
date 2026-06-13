# Test Path & Sequencing — NellDB

Scope: Map out where tests go and in what order to fix Issues 2, 4, 5, 6 in the
NellDB repo at `/work/apps/nell-engine`. Read-only recon pass.

---

## 0. Baseline status (no edits, just `go test ./... -count=1`)

The full suite is **currently red** on a fresh run:

```
ok      github.com/samcharles93/NellDB                        0.018s
--- FAIL: TestWASMCallbacks (0.05s)                           <-- client/wasm_test.go:17
        build WASM client:
        client/main.go:145:15: cannot use nell.NewIndexedDBStore()
        (value of type *nell.IndexedDBStore) as nell.Store value
        in assignment: *nell.IndexedDBStore does not implement
        nell.Store (missing method NodeID)
--- FAIL: TestWASMBuild (0.04s)                                <-- cmd/nelldb-server/build_test.go:42
        WASM build failed:  (same error as above)
ok      github.com/samcharles93/NellDB/logstore
ok      github.com/samcharles93/NellDB/sdk
ok      github.com/samcharles93/NellDB/server
FAIL    github.com/samcharles93/NellDB/client
FAIL    github.com/samcharles93/NellDB/cmd/nelldb-server
```

`go vet ./...` and `staticcheck ./...` pass cleanly. `go test ./client`,
`go test ./cmd/nelldb-server` (which contain the only failing tests) red;
everything else green.

**Root cause:** partial Issue 6 work. `indexeddb_wasm.go` was refactored to a
no-arg `NewIndexedDBStore()` (and a new `meta` object store for the persistent
UUID), but (a) it lost the `NodeID()` method the `nell.Store` interface
requires and (b) `client/main.go` still calls
`nell.NewIndexedDBStore("wasm-client")` against the new signature. The whole
package is `//go:build js && wasm`, so the broken code only surfaces when the
WASM target is built — which `make test-wasm` and
`TestWASMBuild` both do.

**Implication:** Issue 6 is a hard prerequisite for the test suite to go
green. Do it first, regardless of where it sits in the user's mental
priority.

---

## 0a. Repository artifacts the recon surfaced

- `Makefile` (root): only `build-wasm`, `build-server`, `test-wasm`, `clean`.
  No additional test-related targets beyond what `AGENTS.md` lists.
- `cmd/nelldb-server/build_test.go` (8 tests, ~140 lines) — enforces CGO=0,
  WASM build, `go mod tidy`, `go vet`, no `"C"` imports, valid
  `go:generate`, runtime shim presence, dependency-count budget (≤35).
  Treat this file as a **first-class** test target: anything that breaks
  WASM build or `go vet` will fail it.
- `client/wasm_test.go` — runs the WASM binary under Node via
  `wasm_exec.js` and exercises the JS callbacks. Skip if `node` is absent.
  Same WASM-build error here.
- `offensive_test.go` at repo root — package `nell`, **446 lines, 30
  functions**. Concurrency (concurrent puts, list+put, delete+get, race
  detection), HLC monotonicity + tie-breaks, `KnowledgeVector` semantics,
  LWW tie-break determinism (incl. an "ordering-doesn't-matter" test that
  runs `ResolveConflict(a,b)` and `ResolveConflict(b,a)` and asserts equal
  winners), `MemoryStore` edge cases (empty IDs, future clocks, large
  payloads, many records). Files of the same name exist under `logstore/`
  with the same structure (corrupted header, truncated frame, stress,
  concurrency).
- `logstore/offensive_test.go` — **7509 bytes, 12 functions**. Corruption
  tolerance (empty file, truncated frame, corrupt header, zero-length
  frame), concurrency, knowledge-vector survival across reopen.
- `server/server_test.go` — **14 + 10 tests across HTTP + mesh**, no
  WebSocket coverage yet (the `/sync/ws` handler has no test).
- `sdk/docdb_test.go` — **17 tests** across CRUD, conflict, AllDocs,
  changes feed, replicator roundtrip, idempotent pull, persisted
  last-seen-clock, and one Live-loop test (`TestReplicatorLivePicksUpNewRecords`).
  No WebSocket-client test.

### Reuse pattern observed across the suite

All in-process integration tests use `httptest.NewServer` wrapping
`server.New(store, nodeID).Handler()` — see the `newTestServer` helper in
`sdk/docdb_test.go:471-477` and the identically-named one in
`server/server_test.go:20-26`. New tests for the WebSocket-driven paths
should use `httptest.NewServer` + `gorilla/websocket.Dialer` against the
same `/sync/ws` endpoint, which is already wired
(`server/main.go:81` + handler at `server/main.go:265`).

---

## 1. Per-issue: test location, validation commands, smallest vs.
comprehensive, deliberate failure mode

### Issue 2 — IndexedDBStore has zero tests; need fake-indexeddb wired into
the Node harness

**Current state.** The only IndexedDBStore tests today are
`client/wasm_test.go:11`, which runs the *real* WASM binary against the
*real* `indexedDB` global in a headless Node process. That exercises
happy-path Put/Get/AllDocs/Remove through JS callbacks, but it does not
test:

- `Delete` roundtrip
- `GetChangesSince` (cursor iteration, the "clock" index)
- Persistence across `Close` + reopen
- Conflict resolution (the LWW path through IndexedDB)
- Corrupt-version upgrade from v1 (records-only) to v2 (records + meta)
- `resolveOrCreateNodeID` round-trip — currently the
  `client/main.go:145` build is broken so even the happy path doesn't
  run

**Where the tests go.**

- `client/indexeddb_test.go` (new file) — table-driven Go tests that run
  under the **WASM build tag** and a real `indexedDB` shim. Use the
  existing `client/wasm_test.go` harness pattern (Node + fake-indexeddb
  shim from `npm:fake-indexeddb`).
- Alternative: factor a JS-free `Store` driver behind an interface in
  `indexeddb_wasm.go` and unit-test the `ResolveConflict` / cursor / key
  wiring on `js.Value` mocks. Lower fidelity, but pure Go and runs in
  `go test ./...`. Recommended only as a stepping stone.

**Validation commands.**

```
# smallest — proves the build is healthy and the happy path still works
go test ./client -run '^TestWASMCallbacks$' -count=1
make test-wasm

# comprehensive — adds the new test file
go test ./client -count=1
go test ./... -count=1    # ensures cmd/nelldb-server's TestWASMBuild also green
```

**Smallest meaningful test.** `TestIndexedDBStorePutGetRoundtrip` — open
the store with version 2, `Put` a record, `Get` it back, assert identity.
Proves the upgrade path from v1 isn't broken and the JS bridge still
works.

**Comprehensive table-driven test.** `TestIndexedDBStoreTable` with cases
for: roundtrip, overwrite (LWW), delete (tombstone), GetChangesSince
(cursor), reopen-and-read (persistence), and the v1→v2 upgrade. Faking
out the indexedDB global from a Node shim is what
[`fake-indexeddb`](https://github.com/dumbmatter/fakeIndexedDB) does —
it must be added as a `node_modules` dep under `client/`, since it's
not a Go module and is only used by the Go test process.

**Deliberate failure mode to test.** Force a v1 database (object store
"records" only, no "meta") and reopen with the v2 code. The `upgradeneeded`
handler must create the "meta" object store without losing the existing
records. Also: write a corrupt record into the IDB store (missing
required JSON field), reopen, ensure `Get` returns an error rather than
panicking.

---

### Issue 4 — Replicator Live mode is a 5-second polling loop; need WS push
on the client side

**Current state.** `Replicator.Live` in
`sdk/replicate.go:176-219` is a `time.Timer` loop: every `cfg.Interval` it
calls `Push` then `Pull`. The server already broadcasts on its WS
endpoint (`server/main.go:81`, `handleWebSocket` at
`server/main.go:265-336`, `broadcast` at
`server/main.go:343-355`), but **no client consumes it**.

**Where the tests go.**

- `sdk/replicator_ws_test.go` (new) — uses
  `httptest.NewServer(srv.Handler())` with `ws://` URL and a
  `gorilla/websocket.Dialer` against `/sync/ws?node_id=…`. Asserts that
  `Replicator.Live(wsDialer)` ingests records pushed by the server
  within a sub-second budget.
- `client/wasm_test.go` (extend existing) — drives the WASM binary to
  connect to a local test server over `ws://` and observe
  `nellSync`/new `nellSubscribe` callback firing. This is the integration
  leg that proves the JS end of the bridge is correct.

**Validation commands.**

```
# smallest — proves the SDK WS consumer works end-to-end against an
# in-process server
go test ./sdk -run '^TestReplicatorWS' -count=1

# full
go test ./sdk -count=1
go test ./... -count=1
```

**Smallest meaningful test.**
`TestReplicatorWSLiveArrivesUnder1s` — start an httptest server with
the handler, build a `Replicator` configured for WS, push a record into
the server's store from a third party, assert the local DB sees it in
under 1s. Compare to the polling path
(`TestReplicatorLivePicksUpNewRecords` in `sdk/docdb_test.go:433`) which
takes `cfg.Interval` (default 5s) plus network round-trips.

**Comprehensive test.** A table-driven
`TestReplicatorWSTable` with cases: (a) baseline push-then-receive,
(b) reconnect after server restart (dial URL twice, second connection
picks up missed records via the initial vector sweep), (c) backpressure
(server broadcasts 100 records rapidly, client doesn't drop), (d)
graceful shutdown (`Live.Stop()` unblocks the read loop, no goroutine
leak — use `runtime.NumGoroutine` or `goleak` if available, otherwise
just assert the stop callback returns).

**Deliberate failure mode to test.** Two writers race to the same doc
on the server, both with identical HLC. The WS path must still resolve
deterministically via LWW (lexical `UpdatedBy` tie-break) — i.e. the
WS path must *not* introduce a different conflict policy than the
polling path. Assert the WS-ingested and Pull-ingested copies converge
to the same winner.

---

### Issue 5 — `_rev` is theater; silent LWW. Need onConflict or 3-way merge

**Current state.** `sdk.DocDB.Put` checks `_rev` for stale writes and
returns `ErrConflict` (`sdk/docdb.go:151-156`). Good. But
`sdk.DocDB.ingestRemote` (the path used by both `Pull` and the
replicator's WS variant) calls `store.Put(rec)` and trusts the engine's
`ResolveConflict` to pick a winner — the local losing copy is silently
overwritten, and there is no callback. The `onConflict` JS hook exposed
in `client/nell.js:122` is dead code. The `onConflict` lifecycle method
on the JS class is not invoked from any WASM callback.

**Where the tests go.**

- `sdk/conflict_test.go` (new) — pure-SDK tests, no server.
  - `TestIngestRemoteSurfacesConflict` — local doc at rev 2, remote
    arrives at rev 2, `onConflict` callback fires with both bodies.
  - `Test3WayMergeCounter` — both sides incremented a counter; merged
    doc has the sum.
  - `TestConflictCallbackDefaults` — no callback registered, behaviour
    falls back to current LWW (backward-compat).
- `server/server_test.go` (extend) — confirm the HTTP `/sync/push` and
  WS paths still behave identically when the client has registered an
  `onConflict` resolver.

**Validation commands.**

```
go test ./sdk -run '^TestIngestRemote|^Test3Way|^TestConflictCallback' -count=1
go test ./sdk -count=1
go test ./... -count=1
```

**Smallest meaningful test.**
`TestIngestRemoteInvokesOnConflict` — set up `DocDB` with
`OnConflict(func(local, remote Doc) (Doc, error) { return merge(local, remote) })`,
push a doc, simulate a peer pushing a divergent copy via
`DocDB.ingestRemote`, assert the merged doc wins and the original losing
copy is reachable through the callback for audit.

**Comprehensive test.** A table-driven
`TestConflictPolicyTable` with one case per policy: (a) default LWW (no
callback), (b) `LastWriteWins` policy (explicit), (c) `ThreeWayMerge`
policy for `{a: counter}` style fields, (d) `AppDefined` policy that
returns an error (caller must surface this), (e) tombstone-vs-live
conflict, (f) live-vs-tombstone conflict.

**Deliberate failure mode.** Two clients edit the same doc offline
(client A adds a tag, client B adds a different tag), then
reconcile via `Sync`. Today: the silent LWW loser is lost. The fix
must: (1) detect that the local and incoming `_rev` are siblings
(both `>1-<hash>` and not in the same chain), (2) fire `onConflict`
with both bodies, (3) accept the merged body, (4) preserve a
`ConflictHistory` log (or document the lack of one). Assert the
network round trip + merge does not produce a *strict* loss of any
unique field from either side.

---

### Issue 6 — Hardcoded `wasm-client` NodeID; need a persistent UUID per
client

**Current state.** Half-done and *currently broken*:

- `client/main.go:145` calls `nell.NewIndexedDBStore("wasm-client")`.
- `indexeddb_wasm.go:18` declares `func NewIndexedDBStore() (*IndexedDBStore, error)`
  with no args; the store resolves its own nodeID from the IDB "meta"
  object store via `resolveOrCreateNodeID` (`indexeddb_wasm.go:129`).
- The `meta` object store is created in the v1→v2 upgrade handler
  (`indexeddb_wasm.go:67`).
- `uuid_wasm.go` provides `GenerateUUIDv4` via `crypto.getRandomValues`.

What's missing for the contract to hold:
1. `NodeID() string` method on `*IndexedDBStore` — required by
   `nell.Store` (see `store.go:26`). Currently absent → fails
   `*IndexedDBStore` interface assertion.
2. The `client/main.go:145` call site is still passing the old
   `"wasm-client"` string; the compiler error currently masks it.

**Where the tests go.**

- `client/indexeddb_nodeid_test.go` (new) — Node-harness test:
  - Open the store, `Put` a record, `Close`, reopen, assert the new
    store's `NodeID()` matches the old one's (persisted UUID survives
    reload).
  - Same harness, but use `fake-indexeddb` with `indexedDB.deleteDatabase`
    between launches to prove that *different browser profiles* get
    *different* nodeIDs.
- `client/uuid_wasm_test.go` (new, build-tag `js && wasm`) — basic
  smoke: `GenerateUUIDv4` returns 36 chars with the right shape
  (`8-4-4-4-12` hex, version nibble = 4, variant = `10xx`).
- `client/wasm_test.go` (extend) — after the existing put/get/delete
  block, assert `nellNodeID()` returns a non-empty string and that
  closing and re-instantiating the WASM yields the same nodeID
  (within one Node process, before `indexedDB.deleteDatabase`).

**Validation commands.**

```
# smallest — proves the build is unblocked
go test ./client -run '^TestWASMCallbacks$|^TestWASMNodeID' -count=1
go test ./cmd/nelldb-server -run '^TestWASMBuild$' -count=1
make test-wasm

# full
go test ./... -count=1
```

**Smallest meaningful test.**
`TestIndexedDBStoreNodeIDPersistsAcrossReopen` — instantiate the store,
read `NodeID()`, drop the WASM instance, re-instantiate, read `NodeID()`,
assert equality. Proves the v2 schema is wired and the upgrade path
preserves the meta store.

**Comprehensive test.** A table-driven
`TestNodeIDPolicyTable`:
1. Cold start — `indexedDB.deleteDatabase("NellDB")` then
   `NewIndexedDBStore()` → fresh UUID v4 (assert format).
2. Warm reopen — no `deleteDatabase` → returns the same UUID.
3. Two stores side by side in the same DB but different keys (only
   possible if you can name the meta key, e.g. by adding a constructor
   arg) — assert they get distinct IDs.
4. UUID v4 format invariant: regex / `[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}/`.

**Deliberate failure mode.** Tamper with the persisted `meta/node_id`
record (write a non-UUID string into the "value" field), reopen the
store. The fix must *detect* the corruption and regenerate, *not* stamp
that bogus string on every record (which would silently re-create the
original bug for one user).

---

## 2. Dependency graph (which fix unblocks which)

```
                       ┌─────────────────────────┐
                       │ Issue 6 (persistent     │
                       │ UUID + NodeID() method)  │
                       │   UNBLOCKS              │
                       │   TestWASMBuild         │
                       │   TestWASMCallbacks     │
                       └──────────┬──────────────┘
                                  │
                                  ▼
                       ┌─────────────────────────┐
                       │ Issue 2 (IndexedDB tests│
                       │ with fake-indexeddb)    │
                       │   UNBLOCKS              │
                       │   full WASM regression  │
                       │   coverage of the        │
                       │   IndexedDB path         │
                       └──────────┬──────────────┘
                                  │
                                  ▼
                       ┌─────────────────────────┐
                       │ Issue 5 (conflict API /  │
                       │ 3-way merge)            │
                       │   UNBLOCKS              │
                       │   offline-edit tests    │
                       │   across 2+ peers       │
                       │ (needs Issue 6 so the   │
                       │  lexical tie-break is    │
       Issue 4 has no  │  exercised, not papered │
       dependency on   │  over by a shared       │
       2, 5, or 6      │  "wasm-client" alias)   │
                       └──────────┬──────────────┘
                                  │
                                  ▼
                       ┌─────────────────────────┐
                       │ Issue 4 (WS push on the │
                       │ client side)            │
                       │   UNBLOCKS              │
                       │   sub-second replication│
                       │   latency assertions    │
                       │   (benefits from 5,     │
                       │    since push surfaces  │
                       │    silent losses faster)│
                       └─────────────────────────┘
```

**Reading the graph:**

- **Issue 6 is a hard prerequisite.** The test suite is *already red* on
  master because of it. Nothing else can land and stay green until the
  build is unblocked.
- **Issue 2 is structurally independent** but it cannot meaningfully
  land until 6 is fixed, because the WASM build it depends on is
  broken.
- **Issue 5 has a soft dependency on 6.** With a hardcoded
  `wasm-client` nodeID, two WASM clients in the same browser profile
  *and* two servers in the same datacenter would stamp identical
  UpdatedBy on their records, hiding real LWW bugs behind the
  lexical tie-break. Fixing 6 first means 5's tests actually
  exercise deterministic convergence, not coincidental convergence
  via shared identity.
- **Issue 4 has no compile-time dependency on the others.** It is
  *better* tested with 5 in place, because WS push will surface
  silent-LWW losses within seconds, not minutes. But it can land
  first and be tested with deterministic records where the
  LWW outcome is known.

**Two "land together" pairs to call out:**

- 5 + 4 pair naturally because Issue 4's tests need a "policy under
  test" and Issue 5's tests need a "fast path to observe losses on."
- 6 + 2 are the *minimum* to get the WASM build green and at least one
  real IndexedDB test exercising the new schema.

---

## 3. Recommended PR order

**PR 1 — Finish Issue 6 (small, blocking).** Add `NodeID()` method to
`*IndexedDBStore`, update `client/main.go:145` to
`nell.NewIndexedDBStore()`, expose a JS callback (`nellNodeID()`).
**Acceptance:** `go test ./...` is green for the first time on a
fresh run, `make test-wasm` passes. New test:
`TestIndexedDBStoreNodeIDPersistsAcrossReopen` in
`client/indexeddb_nodeid_test.go`.

**PR 2 — Issue 2 tests (no production change).** Add
`fake-indexeddb` to the Node harness, write the table-driven
`TestIndexedDBStoreTable` in `client/indexeddb_test.go`. No changes
to `indexeddb_wasm.go` or `client/main.go`. **Acceptance:**
`go test ./client -count=1` runs the new IndexedDB tests, all pass.

**PR 3 — Issue 5 (SDK onConflict + 3-way merge).** Add an
`OnConflict` registration method on `*DocDB`, wire it into
`ingestRemote`, define a `ConflictPolicy` interface, ship two
reference policies (`LastWriteWins`, `ThreeWayMerge`). New tests in
`sdk/conflict_test.go`. Expose the `onConflict` JS hook in
`client/main.go` so `client/nell.js:122` stops being dead code.
**Acceptance:** All existing tests still pass; the new
`TestIngestRemoteSurfacesConflict` and
`Test3WayMergeCounter` pass; `staticcheck ./...` clean.

**PR 4 — Issue 4 (Replicator Live WS push).** Add a `Replicator` mode
that subscribes to the server's `/sync/ws` endpoint and ingests
broadcasts immediately, falling back to the polling loop on
disconnect. New test: `TestReplicatorWSLiveArrivesUnder1s` in
`sdk/replicator_ws_test.go`. Expose a `nellSubscribe(serverUrl)` JS
callback. **Acceptance:** new sub-1s latency test passes; existing
`TestReplicatorLivePicksUpNewRecords` (5s polling baseline) still
passes for backward compat.

Each PR is reviewable independently. PR 1 is the smallest (a missing
method, a call-site fix, one new test). PRs 2-4 are additive (new
files, new symbols). None of them require coordinated changes across
multiple packages in a way that would block a rebase.

---

## 4. End-to-end smoke after all four land

Once all four PRs are merged, the proof of reliability is a
five-step sequence. None of these need new test code — they
re-exercise what the four fixes unlocked.

```
# 1. Build is green on a fresh checkout
go test ./... -count=1
go vet ./...
staticcheck ./...
make test-wasm
```

Expected: zero failures (Issue 6 fix unblocks `TestWASMBuild` and
`TestWASMCallbacks`; Issues 2/4/5 add passing tests rather than
breaking existing ones).

```
# 2. End-to-end: WASM client with persistent UUID survives reload
go test ./client -run '^TestIndexedDBStore' -count=1 -v
```

Expected: nodeID-persistence test passes, table-driven IDB store
tests pass.

```
# 3. Conflict path: two offline edits merge instead of one
# being silently dropped
go test ./sdk -run '^TestConflict|^Test3Way' -count=1 -v
```

Expected: callback fires, no field loss in the round-trip.

```
# 4. Replication latency: WS path beats polling by an order of
# magnitude on the same machine
go test ./sdk -run '^TestReplicatorWS' -count=1 -v
```

Expected: sub-second ingest asserted, server-to-client end-to-end
under 1s for at least one record.

```
# 5. CLI smoke: start the server, push, pull, kill, restart, pull
# (mirrors the README's "Verified end-to-end" check, but with the
# upgraded engine)
go build -o bin/nelldb-server ./cmd/nelldb-server/
./bin/nelldb-server --in-memory --addr :19342 &
SERVER=$!
curl -sS -X POST :19342/sync/push \
  -d '{"changes":[{"id":"smoke","type":"text","payload":"aGk=","clock":{"wall_time":1,"counter":0},"updated_by":"cli"}]}'
curl -sS -X POST :19342/sync/check \
  -d '{"sender_node_id":"cli","vector":{}}' | jq .missing_changes[0].id
# expect: "smoke"
kill $SERVER
```

A reviewer can run this entire sequence in under two minutes and
have high confidence that Issues 2, 4, 5, and 6 are all closed.

---

## 5. Open questions / risks for the implementer

1. **`fake-indexeddb` as a Node dep, not a Go module.** It has to
   live under `client/node_modules/` and be loaded by the Go test
   process's `exec.Command("node", ...)`. Confirm the Go test
   harness is happy to `cd client && npm i` once, or hard-link
   `node_modules` into the test temp dir.
2. **`IndexedDBStore` interface gap.** Adding `NodeID()` is trivial,
   but the server's `seedKnowledgeVector`
   (`server/main.go:54-72`) also probes an optional
   `KnowledgeVector()` method on the store. Decide whether the IDB
   store should expose a non-trivial KV (it'd need a new IDB object
   store for the vector) or return an empty one and rely on the
   fallback scan. Default to empty + scan for now; revisit in a
   follow-up.
3. **JS-side `onConflict` wiring.** Today the JS class in
   `client/nell.js:122` defines `onConflict(cb)`, but the WASM
   exports don't read it. Decide whether the policy lives in Go
   (passed at construction) or in JS (called by Go on every
   conflict). Go-side is simpler; JS-side is more flexible.
4. **WS auth.** `handleWebSocket` (`server/main.go:280-285`) reads
   `node_id` from the query string with no auth. If
   `HMACAuth` middleware is mounted, the upgrade request bypasses
   it. Decide whether WS gets the same HMAC treatment before
   shipping.
5. **Goroutine leak in `Live` test.** The existing
   `TestReplicatorLivePicksUpNewRecords` relies on `t.Context()`
   for cancellation. The new WS variant should also drain cleanly
   on `Stop()`; assert `runtime.NumGoroutine()` returns to baseline.

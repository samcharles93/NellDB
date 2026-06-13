# Research: NellDB external evidence pass (Issues 2, 4, 5, 6)

> Repo under review: NellDB — Go engine + HTTP sync server + Go/WASM/JS browser
> client using IndexedDB, with `Replicator`, HLC, and LWW conflict resolution.
> Scope: four issues raised for the reliability / conflict-handling pass.
> No files were edited. Sources are linked inline. Dates reflect what was
> visible on the web at the time of research (current date 2026-06-08).

---

## Summary

NellDB is a real, well-shaped local-first sync engine whose four outstanding
issues are all solvable with mainstream 2024–2026 tooling. IndexedDB
behaviour can be exercised in a Go-WASM Node test by polyfilling the
`indexedDB` global through `fake-indexeddb` via `node --require`; cross-node
conflict handling should grow an `onConflict`/`3-way-merge` hook in front of
the existing engine LWW; the 5s polling replicator should be replaced with a
WebSocket client built on Go's `syscall/js` (browser `WebSocket` API); and a
persistent per-client UUID should be generated with `crypto.randomUUID()` and
stored in IndexedDB, not hardcoded as `"wasm-client"`. All four are practical,
well-documented changes — none of them require new theory.

---

## A) Issue 2 — Zero tests for IndexedDBStore

### What we need
A way to run `client/main.go` (built for `GOOS=js GOARCH=wasm`) under Node and
give it an `indexedDB` global backed by a real (in-memory) IndexedDB
implementation, so that the current `client/indexeddb_wasm.go` (or whatever
the WASM-only IndexedDB store is named — see Gap below) actually executes
its real code path. Today the test falls through to `MemoryStore`.

### Authoritative sources (all 2024–2025, verified live)
- **fake-indexeddb** — npm package, currently `v6.2.5` (published 2025-11-07,
  ~3 M weekly downloads, license Apache-2.0, 0 runtime deps). [npmjs.com listing](https://www.npmjs.com/package/fake-indexeddb),
  [GitHub repo](https://github.com/dumbmatter/fakeIndexedDB).
  Maintained by Jeremy Scheff with active contributions from Nolan Lawson.
  v6.0.0 (2024-05-20) switched errors to native `DOMException`; v6.1.0
  (2025-08-08) added `getAllRecords` + descending `getAll`; v6.2.0
  (2025-08-28) added `forceCloseDatabase` and a 13× B-tree perf win on
  inserts with many `multiEntry` indexes. The repo runs the W3C Web
  Platform Tests IndexedDB suite and currently passes 1369/1651 ≈ 82.8 %
  (the README has a comparison table updated automatically against
  Chrome/Firefox/Safari baselines on 2025-11-07).
- **fake-indexeddb key-range API** — `IDBKeyRange.lowerBound(value, open?)`,
  `IDBKeyRange.upperBound(value, open?)`, `IDBKeyRange.bound(lo, hi, loOpen?, hiOpen?)`,
  `IDBKeyRange.only(value)`. All four are present and verified against the
  2025 WPT in the Tessl-rendered reference. [Tessl docs](https://tessl.io/registry/tessl/npm-fake-indexeddb/6.2.0/files/docs/key-ranges.md)
- **fake-indexeddb cursor API** — `IDBCursor.continue(key?)`,
  `IDBCursor.continuePrimaryKey(key, primaryKey)`, `IDBCursor.advance(count)`,
  `getAll`, `getAllKeys`, `getAllRecords`, `multiEntry` indexes,
  `nextunique` / `prevunique`. All are present in 6.x. [Tessl docs](https://tessl.io/registry/tessl/npm-fake-indexeddb/6.2.0/files/docs/transactions-cursors.md)
- **Go WebAssembly reference** — Go Wiki page documents
  `GOOS=js GOARCH=wasm go test`, the `go_js_wasm_exec` wrapper, and the
  modern location of the support files at `$(go env GOROOT)/lib/wasm/`
  (rather than the legacy `misc/wasm/`) since Go 1.24. [go.dev/wiki/WebAssembly](https://go.dev/wiki/WebAssembly)
- **StackOverflow walk-through: running a Go-WASM module under Node** —
  shows the full `wasm_exec.js` polyfill + the
  "the global hasn't been set yet, poll for it" race that bites the first
  attempt, plus a Node-side shim that wires `globalThis.crypto` etc. before
  requiring `wasm_exec.js`. [SO #75372474](https://stackoverflow.com/questions/75372474/how-can-i-run-a-go-wasm-program-using-node-js)
- **aperturerobotics/go-indexeddb** — a real, recent fork of
  `hack-pad/go-indexeddb` that already does roughly what NellDB needs
  (type-safe Go bindings to IndexedDB over `syscall/js`) and is tested
  via `wasmbrowsertest`. The README explicitly calls out the
  "Transactions Expiring" gotcha: when a Go goroutine suspends inside a
  `select`, the IndexedDB transaction auto-commits, and you need to
  `RetryTxn` the operation. [GitHub](https://github.com/aperturerobotics/go-indexeddb)

### What we should do (code-level hints)

1. Add `fake-indexeddb@^6.2.5` to a new `client/testdata/package.json`
   (devDependency, not a runtime dep). Install once in CI.
2. Create a small Node bootstrap (`client/testdata/host.js`) that
   `require`s `fake-indexeddb/auto` (which assigns `globalThis.indexedDB`
   and `globalThis.IDBKeyRange`) **before** loading the Go-compiled
   `wasm_exec.js` and the `nell.wasm` artifact.
3. Convert the test into a `make test-wasm` target that:
   - runs `GOOS=js GOARCH=wasm go test ./client/...` with
     `-exec="$(node $REPO/client/testdata/host.js)"` (mirrors the
     `wasmbrowsertest` pattern but inside Node, not a headless
     browser), or alternatively copies `go_js_wasm_exec` to wrap a
     dedicated test binary.
   - the test calls `nellPut`, `nellGet`, etc. (the same global
     callbacks `client/main.go` registers) and asserts on the
     returned JSON.
4. If a `cgo`/no-`cgo` split matters, gate the existing
   `nell.NewIndexedDBStore(...)` lookup with a build tag (`//go:build js && wasm`),
   the same way `client/main.go` already does.
5. Two sharp edges to plan around:
   - **Transaction auto-commit.** A `db.transaction(...)` body in Go that
     hits a `select` (e.g. waiting on a `time.After` or a channel send)
     will silently commit and the next IDB call will fail with
     `"TransactionInactiveError: The transaction has finished."`. Either
     structure the store so each operation opens its own short-lived
     transaction (idb-keyval style), or wrap every multi-statement
     operation in a `RetryTxn` helper (the aperturerobotics package
     shows the shape).
   - **`AddEventListener` in `syscall/js` and goroutine wakeups.**
     Go's `FuncOf` callback runs on a *new goroutine* (per
     `pkg.go.dev/syscall/js` doc), but the event handler must not
     block on JS that needs the event loop (e.g. `http.Get` /
     `fetch`). Plan the test for this by exposing a Go-side
     `await idbOp()` that the test calls from a `go func()` and
     resolves on a `chan error`.
   - `node --require fake-indexeddb/auto` works for the polyfill, but
     remember that `auto` calls `Object.defineProperty` on the
     Node `global` — Node 18+ supports this fine. Confirmed by
     the `dumbmatter/fakeIndexedDB` test suite running under Node 24
     (per the package's own devDependencies).

### Confidence: **high**
The toolchain (fake-indexeddb 6.x, `GOOS=js GOARCH=wasm go test`, Node
`--require`) is well-trodden. The only NellDB-specific unknowns are what
the current `indexeddb_wasm.go` file actually does (I couldn't find it in
the working tree) and whether the Go tests are written in a Go-`testing`
style or as standalone Go programs.

### Gaps
- I could not read `client/indexeddb_wasm.go` or `sdk/replicator.go`
  in the local working tree — those paths 404'd. The AGENTS.md confirms
  both exist and a "TODO around full sync" exists, but I don't know
  the exact IDBObjectStore layout (which keypath, which index names,
  whether `multiEntry` is used) that the test must round-trip.
- No public example I could find where a *third-party* Go-WASM program
  *plus* a Go-driven Node test harness uses fake-indexeddb. The closest
  precedent is `aperturerobotics/go-indexeddb` using
  `wasmbrowsertest` (real Chrome, not Node), and `dumbmatter/fakeIndexedDB`
  itself (a JS project). The combination of Go WASM + Node +
  fake-indexeddb is novel but mechanically trivial.

---

## B) Issue 5 — `_rev` is theater and causes silent data loss

### What we need
The SDK's `_rev` field only protects against stale writes on a single node.
Across nodes, the engine in `nell/store.go` `ResolveConflict` does pure
HLC-then-`UpdatedBy` LWW. For a note-taking app, that means two users
editing the same note on different devices, then syncing, will have one
side's edits silently dropped at the field granularity — there is no
`onConflict` callback, no per-field merge, and no versioned payload.

### Authoritative sources (all 2014–2026, verified live)

- **Apache CouchDB 3.5 docs: Replication and conflict model.**
  The definitive description of multi-master revision trees,
  `?conflicts=true` / `?open_revs=all`, the deterministic
  "longest rev-history list, then ASCII `_rev` sort" winner algorithm,
  and the explicit guidance that **"CouchDB does not attempt to merge
  the conflicting revision. Your application dictates how the merging
  should be done."** [docs.couchdb.org](https://docs.couchdb.org/en/latest/replication/conflicts.html)
- **The definitive guide: Conflict Management.** Same algorithm,
  with a clear "Application-specific merging" recommendation and the
  rule-of-thumb "if both children are in conflict, the deterministic
  algorithm determines the winner — every replica picks the same
  one — and the application can then write back a merged revision
  that *deletes* the losing leafs via `_bulk_docs`." [guide.couchdb.org/draft/conflicts.html](https://guide.couchdb.org/draft/conflicts.html)
- **PouchDB Conflicts guide.** PouchDB is the JS-ergonomics mirror of
  CouchDB and treats conflicts exactly the same. The "App dictates
  the merge" line and the "Accountants don't use erasers" anti-pattern
  (never update, only append) come from here. [pouchdb.com/guides/conflicts.html](https://pouchdb.com/guides/conflicts.html)
- **Reintech blog: Handling Conflicts and Concurrency Control in CouchDB**
  (third-party but well-aligned). Practical strategies including
  field-level merge, "splitting a fat document into focused documents
  to reduce conflict surface," and vector-clock fields for
  causality. [reintech.io/blog/handling-conflicts-concurrency-couchdb](https://reintech.io/blog/handling-conflicts-concurrency-couchdb)
- **RFC 7396: JSON Merge Patch.** IETF Standards Track, Hoffman &
  Snell, 2014. The canonical 3-rule algorithm: `null` deletes a key,
  present keys recurse, everything else replaces. Authoritative
  text. [rfc-editor.org/info/rfc7396](https://www.rfc-editor.org/info/rfc7396/)
  Note the **critical limitation**: it replaces arrays wholesale —
  no way to append a single element. [jsonic.io/guides/json-merge-patch](https://jsonic.io/guides/json-merge-patch)
- **RFC 6902: JSON Patch.** IETF, 2013. Array-element operations,
  `test` for optimistic assertions. [datatracker.ietf.org/doc/rfc6902](https://datatracker.ietf.org/doc/html/rfc6902)
- **JSON Patch vs JSON Merge Patch comparison.** Both formats;
  Merge Patch is the right default for "set a few top-level fields";
  Patch is the right choice for arrays and conditional updates.
  [erosb's blog](https://erosb.github.io/post/json-patch-vs-merge-patch/),
  [jsonic.io/guides/json-concurrent-updates](https://jsonic.io/guides/json-concurrent-updates)
- **Automerge (the CRDT reference implementation).** MIT, 6.1k stars,
  Rust core compiled to WASM, JS bindings; **v3 released 2024 with a
  ~10× memory reduction**. Stable JS API (`@automerge/automerge`). The
  the binary format is spec'd at [automerge.org/automerge-binary-format-spec](https://automerge.org/automerge-binary-format-spec/).
  [github.com/automerge/automerge](https://github.com/automerge/automerge/)
- **Yjs (the high-performance CRDT for text/rich).** MIT, 17k stars.
  Shared types `Y.Map`, `Y.Array`, `Y.Text`; binary sync protocol
  (`y-protocols`) using a state vector for `SyncStep1`/`SyncStep2` and
  incremental `Update` messages; Awareness for presence/cursors.
  [docs.yjs.dev/ecosystem/connection-provider/y-websocket](https://docs.yjs.dev/ecosystem/connection-provider/y-websocket),
  [PROTOCOL.md](https://github.com/yjs/y-protocols/blob/master/PROTOCOL.md)
- **Go CRDT bindings.**
  - `github.com/automerge/automerge-go` — featureful wrapper over
    `automerge-rs` via cgo. 126 stars, last push 2024-10. [GitHub](https://github.com/automerge/automerge-go)
  - `github.com/develerltd/go-automerge` — **pure Go**, no cgo,
    binary-compatible with the Rust automerge v0.8.0, 2026-03
    project. Tracks upstream. [GitHub](https://github.com/develerltd/go-automerge)
  - For Yjs there is no first-class Go binding — most Yjs-in-Go
    projects use `y-crdt` via wazero/WASI (see
    `joeblew999/automerge-wazero-example` for a working pattern using
    Automerge). [GitHub](https://github.com/joeblew999/automerge-wazero-example)
- **Replicache — concrete reference for "poke + rebase".** Documents
  the "Push, Pull, Rebase" loop, the `clientID` / `clientGroupID`
  identifiers, and the recommended transport for *real-time hints*:
  "Any hosted WebSocket service like Pusher or PubNub works. You can
  also implement your own WebSocket server or use server-sent
  events. And some databases come with features that can be used for
  pokes." [doc.replicache.dev/concepts/how-it-works](https://doc.replicache.dev/concepts/how-it-works)
- **Replicache: Poke design.** Pokes carry no data; the client then
  calls `pull()`. Suggests using `pullInterval` defaults of 60 s plus
  per-document pubsub channels. [doc.replicache.dev/byob/poke](https://doc.replicache.dev/byob/poke)

### What we should do (code-level hints for a note-taking app)

For a notes app where the doc shape is `{title, body, tags, attachments}`,
the **pragmatic layered approach** that lines up with the AGENTS.md
boundary is:

1. **Layer 1 — Keep engine LWW for binary safety.** Don't try to
   remove `ResolveConflict` in `nell/store.go`; it correctly converges
   replicas deterministically. Continue to store a synthetic HLC
   per record.
2. **Layer 2 — Add an `OnConflict` hook at the SDK boundary.** When
   the engine returns `accepted=false` (an LWW collision), the SDK
   pulls both the local and incoming `nell.Record` payloads from the
   store, and if both decode as `sdk.Doc` (JSON object), calls a
   caller-supplied `Merge(local, incoming, base) (sdk.Doc, error)`.
   - **Default merge = field-level LWW with per-field HLC.** Each
     leaf scalar in the doc gets a companion `_h:{field}` field that
     is the HLC of the last write to that field. On conflict,
     walk both objects, pick whichever side has the newer HLC for
     each field. This is what the Reintech piece describes and is
     ~50 lines of Go without any new dependency.
   - **Optional richer merge = CouchDB 3-way merge.** Store a
     lightweight per-doc "base" (the common ancestor) alongside
     the doc. On conflict, take the common ancestor and the two
     sides, compute a 3-way diff, and resolve per-field as in
     `pouch-merge` or by hand. This is what CouchDB and PouchDB
     document; the catch is keeping a usable base, which means
     storing one extra `_base` blob per doc — small in notes apps.
   - For arrays of tags/attachments, **don't merge with merge
     patch** (it would replace the whole array — RFC 7396 §2). Use
     union semantics, or treat the array as an *append-only* log
     (PouchDB's "Accountants don't use erasers").
3. **Layer 3 — Surface user-visible conflict UI when both sides
   changed the same field with the same HLC** (essentially the
   "deterministic winner tie" the engine already has). The current
   engine LWW is fine to pick a winner, but the application should
   at least get a *callback* on `Put` collisions so it can present
   a "merge these two" UI. This is the `onConflict` callback the
   user-visible report asks for.
4. **Don't go straight to Automerge/Yjs.** Both are excellent but
   are 2–5× the on-disk size of a plain JSON doc, add a sync
   protocol to maintain, and (for Yjs) need a Go binding via WASI
   (no pure-Go production option exists in 2025). They are the
   right answer for *concurrent text editing* of the same note —
   overkill for "user edits title on phone, edits body on laptop."

### Confidence: **medium-high** for the 3-way-merge + field-HLC plan;
**low** for the Automerge recommendation, which is a bigger commitment.

### Gaps
- I have no way to verify the existing doc shapes used by real NellDB
  callers; the recommendation assumes a `{title, body, tags, ...}` shape.
  If the real app is more like `{blocks: [...]}`, the advice shifts
  toward a CRDT earlier.
- The CouchDB-style "common ancestor base" approach is *unbounded*
  unless you compact. The current NellDB logstore already compacts
  tombstones, so a parallel compaction of `_base` is feasible but I
  can't see the logstore code from this environment.

---

## C) Issue 4 — Replicator Live mode is a 5 s polling loop

### What we need
A WebSocket client in the WASM build that connects to the new
`/sync/ws` endpoint, receives change broadcasts, and falls back to a
REST `/sync/check` (or `/sync/pull`) on reconnect with a "since"
cursor (an HLC, knowledge-vector snapshot, or per-doc last-seen
revision).

### Authoritative sources (all 2019–2026, verified live)

- **PouchDB live replication (`{ live: true, retry: true }`).**
  Documents the canonical pattern for "set this on, never think
  about it again": live replication + automatic exponential backoff
  with a `back_off_function`. Default backoff starts at 0–2000 ms
  random and roughly doubles up to 10 minutes. Events: `change`,
  `paused`, `active`, `denied`, `complete`, `error`, `checkpoint`.
  Also documents the **since-cursor pattern**: every `replicate`
  call has a `since` option that takes a `last_seq` (the change
  feed sequence number); on reconnect you must hand it the last
  `last_seq` you observed or you replay from 0. [pouchdb.com/api.html](https://pouchdb.com/api.html)
- **PouchDB replication guide.** Same model, narrative form.
  [pouchdb.com/guides/replication.md](https://pouchdb.com/guides/replication.html)
- **PouchDB issue #3999 ("Mismatching checkpoints causes
  replication to start over from change 0").** The cautionary tale
  — store your `last_seq` externally (localStorage, IndexedDB)
  rather than trusting only PouchDB's checkpoint doc, because
  mismatched source/target checkpoints will otherwise roll the
  client back to 0. This is exactly the trap a Replicator-Live
  WebSocket rewrite has to avoid. [GitHub #3999](https://github.com/pouchdb/pouchdb/issues/3999)
- **Yjs y-websocket (the modern CRDT reference for change push).**
  Websocket Provider with:
  - `wsconnected`, `wsconnecting`, `shouldConnect`, `synced` booleans
  - `connect()` / `disconnect()` / `destroy()` lifecycle
  - `maxBackoffTime: 2500` ms (configurable), exponential backoff
  - `syncStatus` object with `connected`, `receivedInitialSync`,
    `localUpdatesSynced`, `localUpdatesAge`, `lastMessageAge`,
    `status: 'green' | 'yellow' | 'red'` (3.0+)
  - cross-tab BroadcastChannel optimisation, awareness for
    presence/cursors.
  The `maxBackoffTime` default of 2.5 s and the status string
  convention is now the de-facto standard for local-first WS
  clients. [docs.yjs.dev/ecosystem/connection-provider/y-websocket](https://docs.yjs.dev/ecosystem/connection-provider/y-websocket)
- **Yjs y-protocols.** The wire format is a state-vector handshake
  (`SyncStep1` ↔ `SyncStep2`) followed by incremental `Update`
  messages, plus an Awareness channel. This is a clean blueprint
  for "send me everything since HLC X" and "I am online, please
  send me deltas now." [PROTOCOL.md](https://github.com/yjs/y-protocols/blob/master/PROTOCOL.md)
- **Replicache "Poke" pattern.** Pokes are contentless hints ("hey,
  pull now") sent over WebSockets or SSEs; the client then calls
  `pull()`. Recommended transport: WebSocket for the bidir channel,
  or pubsub-style for fan-out. [doc.replicache.dev/byob/poke](https://doc.replicache.dev/byob/poke)
- **Replicache clientID & clientGroupID.** Each Replicache instance
  generates a random `clientID` on construction; multiple
  instances/tabs in one browser share a `clientGroupID`. This is
  the same pattern NellDB needs for its `UpdatedBy` engine LWW
  (per Issue 6). [doc.replicache.dev/concepts/how-it-works](https://doc.replicache.dev/concepts/how-it-works)
- **Go WASM WebSocket story.**
  - `gorilla/websocket` does *not* work under `js/wasm` —
    `dial tcp: Protocol not available` because the Go `net`
    stack has no TCP. [SO #55750947](https://stackoverflow.com/questions/55750947/websockets-over-webassembly-generated-by-golang)
  - `github.com/coder/websocket` (formerly `nhooyr/websocket`)
    is the right answer — its `internal/wsjs` package is a
    `syscall/js` shim around the browser `WebSocket` API and
    compiles under `//go:build js`. It is what production Go-WASM
    projects (including Pusher-style apps, libp2p over WebSocket)
    use. The README explicitly states "The client side supports
    compiling to Wasm. It wraps the WebSocket browser API."
    [github.com/coder/websocket](https://github.com/coder/websocket),
    [internal/wsjs/wsjs_js.go](https://github.com/coder/websocket/blob/master/internal/wsjs/wsjs_js.go)
  - Or you can do the syscall/js shim yourself: ~40 lines, no
    dependency, mirrors the pattern in `coder/websocket`. The
    coder/websocket `wsjs.go` source is the reference
    implementation.
- **Go `syscall/js` and `addEventListener`/`binaryType`.**
  - The browser `WebSocket.binaryType` defaults to `"blob"`.
    `coder/websocket` explicitly sets it to `"arraybuffer"` so
    that it can `js.CopyBytesToGo` synchronously. This is the
    sharp edge in Go WASM: you cannot `js.CopyBytesToGo` from a
    `Blob` — you must opt into `arraybuffer` mode at construction
    time. [coder/websocket wsjs.go](https://github.com/coder/websocket/blob/master/internal/wsjs/wsjs_js.go)
  - `addEventListener("open" | "close" | "error" | "message", cb)`
    is the right call; do *not* use the `onopen = cb` form — it
    can't be `Release()`'d by Go. The `coder/websocket` `addEventListener`
    helper returns a `remove func()` that handles `f.Release()`,
    which is essential to avoid goroutine leaks.
  - The known Go-`syscall/js` deadlock: a `FuncOf` callback must
    not block on the JS event loop (e.g. calling `http.Get`),
    otherwise the event loop itself starves. Reference:
    [golang/go #37136](https://github.com/golang/go/issues/37136)
    — the fix is to spawn a `go func()` for the blocking work and
    deliver the result back via a channel + `js.Func.Invoke`.

### What we should do (code-level hints)

1. Pick `coder/websocket` as the dependency. It already
   special-cases `//go:build js` and shims the browser `WebSocket`,
   so the WASM build needs no special wiring beyond the
   `binaryType = "arraybuffer"` it already sets.
2. In `sdk/replicator.go`, add a `Replicator.Live(ctx, serverURL)`
   method that:
   - On connect: sends `{since: lastSeenHLC}` (HLC is already the
     engine's natural cursor; no new abstraction needed). If no
     `lastSeenHLC` is known (cold start), send `since: 0`.
   - On `open`: dials `/sync/ws?nodeID={ourID}&since={h:ms:c}`,
     where the server can either stream deltas or reply with a
     `pull since` redirect.
   - On `message`: decode the broadcast (proposed shape: JSON
     `{h, c, id, type, payload}`, where `h:ms:c` is the HLC of
     the change) and apply via `engine.Put(record)`.
   - On `close` / `error`: reconnect with exponential backoff
     `min(2^n * 250ms + jitter, 30s)`, plus a `Retry-After`
     header respect if the server sends one. Add 100–500 ms of
     random jitter to avoid thundering-herd reconnects when many
     tabs come online together (PouchDB's default does this).
   - Persist `lastSeenHLC` in the same `meta:clock` record the
     existing replicator uses (or a new `meta:wslast`), so a
     crashed WASM instance resumes from where it left off and
     never replays from 0 (the PouchDB #3999 lesson).
3. In the server (`server/peer.go` already exists), add
   `/sync/ws` that:
   - upgrades to `gorilla/websocket` (the server side *can* use
     gorilla; the limitation is client-side under wasm),
   - on connect reads the `since` query param, calls
     `engine.GetChangesSince(since)`, sends each as a frame,
   - subscribes to the existing `changesHub` (the AGENTS.md
     confirms a hub exists; the bug is that the client ignores
     it) and broadcasts each change to every connected WS client
     after applying the same HLC + knowledge-vector filtering the
     `/sync/check` endpoint already does.
4. If you want to keep the existing `sdk.Replicator.Sync(ctx)`
     poll, expose a `Replicator.Mode` of `"poll" | "ws"` and
     migrate callers one at a time.

### Confidence: **high** for the WS protocol and reconnect logic;
**high** for `coder/websocket` working under `js/wasm`; **medium**
for the specific message format, since `/sync/ws` is new in this
repo and the shape of the server's broadcast hasn't been verified.

### Gaps
- I couldn't see the current `sdk/replicator.go` or the server's
  `changesHub` to confirm what `Change` shape they emit; the
  message format above is my best inference from
  `nell.Record` + the AGENTS.md description.
- I don't know if the Go module is allowed to pull in
  `coder/websocket` (it's a real dep, ~3.7k LoC). If the project
  is allergic to deps, the same effect is ~40 lines of
  `syscall/js` mirroring `coder/websocket/internal/wsjs/wsjs_js.go`.

---

## D) Issue 6 — Hardcoded `wasm-client` NodeID

### What we need
A persistent, per-browser-tab (or per-browser-profile) UUID for the
WASM client that is generated once and survives reloads, used as
the `UpdatedBy` value in the engine LWW so that two tabs on
different machines don't collide on the deterministic tie-break.

### Authoritative sources (all 2021–2026, verified live)

- **MDN: `Crypto.randomUUID()`.** "Baseline Widely available" since
  **March 2022** in browsers, in Web Workers, and a v4 UUID using
  a cryptographically secure RNG. Secure-context only (HTTPS or
  localhost). [developer.mozilla.org/en-US/docs/Web/API/Crypto/randomUUID](https://developer.mozilla.org/en-US/docs/Web/API/Crypto/randomUUID)
- **W3C WICG uuid spec (31 December 2021).** The normative
  definition: `[SecureContext] DOMString randomUUID();` on the
  `Crypto` interface. Uses 16 random bytes laid out per
  RFC 4122. [wicg.github.io/uuid](https://wicg.github.io/uuid/)
- **caniuse.com Web Cryptography.** Browser support matrix
  including `randomUUID`: Chrome 92+, Edge 92+, Firefox 95+,
  Safari 15.4+, all 2022+ mobile. [caniuse.com/cryptography](https://caniuse.com/cryptography)
- **Go `syscall/js` calling `crypto.randomUUID()` in WASM.**
  The standard invocation is:
  ```go
  uuid := js.Global().Get("crypto").Call("randomUUID").String()
  ```
  This works in any Go WASM build because `js.Global().Get("crypto")`
  resolves to the `Crypto` mixin on the global (window in browser,
  `globalThis.crypto` in Node 19+ if you've polyfilled it).
  No npm dep, no `uuid` package, no `Math.random`. Equivalent to
  the pattern `js.Global().Get("crypto").Get("subtle")` used
  elsewhere in Go WASM apps.
- **PouchDB / Replicache device IDs.** Replicache's official
  guidance: "A client is identified by a unique, randomly
  generated `clientID`." Generated on `new Replicache(...)` and
  persisted in the client view; the `name` parameter (cache
  partition key) is *user-supplied* and is the wrong place to put
  the device ID. [doc.replicache.dev/concepts/how-it-works](https://doc.replicache.dev/concepts/how-it-works)
- **IndexedDB for persistence — the right choice for NellDB.**
  IndexedDB survives browser restarts, is durable past the
  localStorage 5–10 MB cap, and is the same store the WASM
  client is already using. PouchDB itself defaults to IndexedDB
  for the same reason ("PouchDB prefers IndexedDB" in the
  browser). [pouchdb.com/adapters.html](https://pouchdb.com/adapters.html)
- **localStorage size limit confirmation.** Multiple sources,
  including the lichess-org PR #8351 (use IndexedDB over
  localStorage for caches > 5 MB) and MDN's localStorage page
  (5–10 MB typical origin cap). localStorage is also
  synchronous and blocks the main thread, which is a real
  problem in a Go-WASM-on-`js/wasm` runtime where the main
  thread is also the only thread you have for IDB transactions.
  [lichess-org/lila #8351](https://github.com/ornicar/lila/pull/8351)
- **URL-based device IDs are an anti-pattern.** The risk of
  accidentally identifying users across sessions and the
  difficulty of un-sharing a device (clear cookies, sign out
  everywhere) make this unsuitable for anything that touches
  sync state.

### What we should do (code-level hints)

1. On first boot, in `client/main.go`:
   ```go
   func loadOrCreateNodeID(ctx context.Context) (string, error) {
       store, err := nell.NewIndexedDBStore("nell-meta")
       if err != nil { return "", err }
       if existing, _ := store.Get("node:id"); existing.ID != "" {
           return existing.UpdatedBy, nil
       }
       uuid := js.Global().Get("crypto").Call("randomUUID").String()
       store.Put(nell.Record{
           ID: "node:id", Type: nell.TypeText, UpdatedBy: uuid,
           Payload: []byte(uuid), Clock: nell.NewHLC(),
       })
       return uuid, nil
   }
   ```
2. Wire the returned string into both `nell.NewIndexedDBStore(uuid)`
   and `sdk.New(store, uuid)` (replacing the two `"wasm-client"`
   string literals that today are in `client/main.go`).
3. Fall back to a non-secure-context UUID if the page is served
   from `http://` (development): if `js.Global().Get("crypto")` is
   undefined, use `js.Global().Get("msCrypto")` (legacy IE/Edge)
   or — last resort — a `crypto.getRandomValues(new Uint8Array(16))`
   + manual RFC 4122 v4 layout. This branch is only hit on
   non-HTTPS dev hosts and should `log.Println` a warning.
4. Do *not* put the node ID in the URL or a cookie. Do *not* use
   `localStorage` (sync, low cap, lives in a different origin
   bucket in some browsers' private-mode setups).
5. For "per-tab" vs "per-profile": if the team wants each tab to
   look like a separate device (more aggressive conflict
   surface, easier to reason about for demos), don't share the
   ID across tabs. If they want "per browser profile, shared
   across tabs" (the PouchDB / Replicache default), use the
   `BroadcastChannel` API plus the same IndexedDB key. PouchDB's
   `pouchdb-adapter-idb` prefix convention (`_pouch_`) is a
   precedent worth copying to avoid colliding with the user's
   other DBs.

### Confidence: **very high** for `crypto.randomUUID()` (it's been
Baseline since March 2022) and for IndexedDB as the storage; **high**
for the syscall/js invocation shape; **medium** for the
tab-vs-profile decision, which is a product call, not a tech one.

### Gaps
- The `localStorage`/5 MB cap is widely cited but the actual
  number is browser-dependent; if someone later argues for
  localStorage on the grounds of "it fits", the indexedDB
  approach is still safer.
- The `client/main.go` code in the working tree (readable from
  the path I have access to) doesn't show the per-tab/profile
  decision, so the recommendation defaults to **per-profile**
  (shared across tabs) which matches the Replicache default.

---

## Sources

### Kept
- **fake-indexeddb** — npm v6.2.5, Apache-2.0, 3 M weekly DLs,
  active maintenance through 2025-11-07, 82.8 % WPT pass rate.
  The single best polyfill for IndexedDB in Node and the only
  one with broad ecosystem traction. [npm](https://www.npmjs.com/package/fake-indexeddb) ·
  [GitHub](https://github.com/dumbmatter/fakeIndexedDB) ·
  [Tessl reference](https://tessl.io/registry/tessl/npm-fake-indexeddb/6.2.0/files/docs/transactions-cursors.md)
- **Go Wiki: WebAssembly** — official Go-team reference for
  `GOOS=js GOARCH=wasm`, `wasm_exec.js`, `go_js_wasm_exec`.
  [go.dev/wiki/WebAssembly](https://go.dev/wiki/WebAssembly)
- **aperturerobotics/go-indexeddb** — real-world Go-WASM
  IndexedDB binding, actively maintained, has the
  "Transactions Expiring" advice and the
  `wasmbrowsertest`-based test setup we'd want to model on.
  [GitHub](https://github.com/aperturerobotics/go-indexeddb)
- **Apache CouchDB 3.5: Replication and conflict model** —
  the canonical description of the deterministic winner
  algorithm and the "application does the merge" pattern.
  [docs.couchdb.org](https://docs.couchdb.org/en/latest/replication/conflicts.html)
- **The CouchDB Guide: Conflict Management** — same
  algorithm in narrative form, with the "common ancestor +
  merge" recipe. [guide.couchdb.org](https://guide.couchdb.org/draft/conflicts.html)
- **PouchDB Conflicts guide** — JS ergonomics mirror of
  CouchDB's merge model; the "Accountants don't use erasers"
  anti-pattern; `pouchdb-upsert` for the read-modify-write
  loop. [pouchdb.com/guides/conflicts.html](https://pouchdb.com/guides/conflicts.html)
- **RFC 7396: JSON Merge Patch** — IETF Standards Track,
  defines the 3-rule algorithm and the `null`-as-delete
  semantics. [rfc-editor.org](https://www.rfc-editor.org/info/rfc7396/)
- **RFC 6902: JSON Patch** — IETF, the array-element +
  `test`-op alternative. [datatracker.ietf.org](https://datatracker.ietf.org/doc/html/rfc6902)
- **jsonic.io guides on JSON Merge Patch and concurrent
  updates** — well-cited, current (May 2026 update), with
  authoritative examples. [JSON Merge Patch](https://jsonic.io/guides/json-merge-patch) ·
  [Concurrent Updates](https://jsonic.io/guides/json-concurrent-updates)
- **Automerge (automerge/automerge)** — 6.1k stars, MIT,
  v3.0 released 2024, stable JS + Rust API. The reference
  CRDT for JSON-like docs. [GitHub](https://github.com/automerge/automerge)
- **Yjs (yjs/yjs + yjs/y-websocket + yjs/y-protocols)** —
  the high-performance CRDT for text/rich with a clean
  WebSocket + state-vector sync protocol that we should
  imitate. [y-websocket](https://docs.yjs.dev/ecosystem/connection-provider/y-websocket) ·
  [PROTOCOL.md](https://github.com/yjs/y-protocols/blob/master/PROTOCOL.md)
- **Go CRDT bindings: `automerge-go` (cgo) and
  `develerltd/go-automerge` (pure Go)** — both available
  if we ever go the CRDT route.
  [automerge-go](https://github.com/automerge/automerge-go) ·
  [go-automerge](https://github.com/develerltd/go-automerge)
- **Replicache "How it works" / "Poke"** — the push/pull/rebase
  loop and the "pokes are contentless hints" pattern, which
  is exactly the architecture NellDB's new `/sync/ws` should
  have. [how-it-works](https://doc.replicache.dev/concepts/how-it-works) ·
  [poke](https://doc.replicache.dev/byob/poke)
- **PouchDB API reference + replication guide** — the
  `live+retry+since` semantics and the documented
  exponential-backoff default (0–2 s → 10 min cap).
  [API](https://pouchdb.com/api.html) ·
  [Replication guide](https://pouchdb.com/guides/replication.html)
- **PouchDB issue #3999** — the "store `last_seq` outside
  PouchDB or risk rolling back to 0" lesson, which is the
  most important warning for the WS-Live rewrite.
  [GitHub #3999](https://github.com/pouchdb/pouchdb/issues/3999)
- **`coder/websocket` (a.k.a. nhooyr/websocket)** — the
  only mainstream Go WS library that compiles under
  `//go:build js`; its `internal/wsjs/wsjs_js.go` is the
  reference syscall/js shim, and it sets
  `binaryType = "arraybuffer"` to dodge the `Blob` →
  `CopyBytesToGo` trap. [GitHub](https://github.com/coder/websocket) ·
  [wsjs.go source](https://github.com/coder/websocket/blob/master/internal/wsjs/wsjs_js.go)
- **MDN: `Crypto.randomUUID()`** — Baseline since 2022-03,
  secure-context only. [MDN](https://developer.mozilla.org/en-US/docs/Web/API/Crypto/randomUUID)
- **W3C WICG uuid spec** — the normative definition.
  [wicg.github.io/uuid](https://wicg.github.io/uuid/)
- **caniuse Web Cryptography** — confirms
  `randomUUID` is universally available in 2022+
  browsers. [caniuse.com](https://caniuse.com/cryptography)
- **StackOverflow "How can I run a Go WASM program using
  Node.js"** — the full wasm_exec.js + Node polyfill
  recipe, including the "exported API not set yet, poll
  for it" gotcha. [SO #75372474](https://stackoverflow.com/questions/75372474/how-can-i-run-a-go-wasm-program-using-node-js)
- **golang/go #37136** — the canonical "don't put
  blocking ops in `js.FuncOf`" reference; the workaround
  is a `go func()` + channel. [GitHub #37136](https://github.com/golang/go/issues/37136)

### Dropped
- **PouchDB older tutorials (pre-`retry: true`, pre-
  `live+retry+heartbeat`)** — superseded by current API
  docs.
- **gorilla/websocket** — does not work under
  `js/wasm`; only relevant for the server side, where
  it's still fine. Mentioned only as a negative result.
- **Hack-pad `localstorage-down` / `localStorage`-based
  device IDs** — explicitly the wrong answer for a Go
  WASM build (sync, low cap, main-thread blocks).
- **URL-based device IDs / cookie-based device IDs** —
  privacy and revocation problems; included only to
  argue against.
- **`uuid` npm package** — superseded by built-in
  `crypto.randomUUID()`; no benefit, adds bundle weight.
- **Replichat examples using a custom random ID via
  `nanoid()`** — fine for a *client ID* but for a Node
  identifier `crypto.randomUUID()` is the more durable
  2025+ choice.
- **Anevok's `automerge-c`/`cgo` path** — not needed
  since `develerltd/go-automerge` is pure Go and
  tracks the same spec.
- **Old `fake-indexeddb` v3/v4 docs** — superseded by
  v6 (Aug 2025+); we should use v6.2+.

---

## What this implies for NellDB

These four issues are tractable and the right 2025-vintage toolchain
exists for all of them. Issue 2 (test the IndexedDB store) is the
smallest change and unblocks the rest: once we have a working Node
test harness around the WASM build, Issue 6 (persistent per-tab
NodeID) is ~30 lines of Go that reads / writes a single
`node:id` record in that same fake-indexeddb store, and Issue 4
(WebSocket Live mode) can be developed against the same harness
with a mock `ws://` server. Issue 5 (real conflict handling) is
architecturally the largest change but should sit cleanly *above*
the existing `nell.ResolveConflict` — keep engine LWW, add an
`OnConflict(local, incoming, base)` hook at the SDK boundary,
default it to field-level LWW with per-field HLC, and make the
`"wasm-client"` tie-break collision impossible by closing Issue 6
first. The honest end state is a notes engine that converges
deterministically today, and — once Issue 5 ships — *also* tells
the application when the deterministic choice is the wrong
semantic answer, which is the right amount of "help" for a
local-first notes app without paying the 2–5× size cost of
Automerge.

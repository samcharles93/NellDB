# ADR 0007: JS SDK and WASM Bridge

- **Status:** Accepted
- **Date:** 2026-06-07

## Context

The Go engine compiles to WASM and runs in browser and Electron environments. JavaScript applications need to call into the engine — put records, query data, control sync — without dealing with WASM instantiation details or raw `syscall/js` glue.

The client Go code (`client/main.go`) already exposes functions via `js.Global().Set()`. A hand-written JS wrapper provides an ergonomic developer experience.

## Decision

### WASM Bridge (`client/main.go`)

Under the `//go:build js && wasm` build tag, the client registers global JS callbacks:

```go
js.Global().Set("nellPut", js.FuncOf(func(this js.Value, args []js.Value) any {
    var rec core.Record
    json.Unmarshal([]byte(args[0].String()), &rec)
    updated, current := store.Put(rec)
    resp, _ := json.Marshal(map[string]any{"updated": updated, "record": current})
    return js.ValueOf(string(resp))
}))
```

The WASM runtime stays alive via `<-ch` in `main()`.

### JS SDK (`client/nell.js`)

The SDK provides a `NellEngine` class that:

1. **Detects the runtime** — Node.js vs browser, to choose `WebAssembly.instantiate()` vs `instantiateStreaming()`.
2. **Loads WASM** — calls the appropriate instantiation method with Go's import object.
3. **Runs the Go runtime** — calls `go.run(instance)` which registers the global callbacks.
4. **Exposes an ergonomic API** — wraps raw `nellPut`/`nellGet` calls in typed methods.

```js
const engine = new NellEngine();
await engine.init('nell.wasm');
const result = engine.put({ id: 'note-1', type: 'text', payload: 'Hello' });
```

The SDK is extended over time with:
- `get(id)` — fetch a single record.
- `delete(id)` — tombstone a record.
- `query(filter)` — list records matching criteria.
- `sync(url)` — connect to a remote server and begin syncing.
- Sync lifecycle hooks: `onConnect`, `onDisconnect`, `onConflict`, `onSyncComplete`.

## Consequences

### Positive

- Developers never touch WASM loading or `syscall/js` directly.
- The same `nell.js` works in React, Electron, Node.js, and Capacitor.
- WASM initialisation is async and idempotent (calling `init()` twice is a no-op).
- The JavaScript surface is small and debuggable.

### Negative

- WASM initialisation is not instant — there's a loading delay while `init()` completes.
- `go.run(instance)` is synchronous in Go's view; the JS event loop continues during WASM execution but Go's main goroutine blocks.
- The JSON serialisation round-trip (JS → JSON string → Go → JSON string → JS) adds overhead for large payloads.

### Mitigations

- Cache the initialised engine instance so subsequent page loads skip re-initialisation.
- For large binary payloads (images), consider direct `syscall/js` byte array passing in future iterations.

---

**Implementation:** `client/main.go`, `client/nell.js`

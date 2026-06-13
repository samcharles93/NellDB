# Expose Document Semantics in WASM/JS Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Update the WASM/JS client interface to expose `sdk.DocDB` document semantics (`_id`, `_rev`, `_deleted`, `put`, `get`, `remove`, `allDocs`) instead of the low-level `nell.Record` interface.

**Architecture:** We will update `client/main.go` to wrap the `nell.MemoryStore` with `sdk.New()`, exporting the new document-centric functions `nellPut`, `nellGet`, `nellRemove`, `nellAllDocs`, and `nellSync`. We will then update `client/nell.js` to provide the clean Javascript API for these functions and update the integration test `client/wasm_test.go` to verify the new semantics.

**Tech Stack:** Go, WebAssembly, JavaScript

---

### Task 1: Update WASM Exports in `client/main.go`

**Files:**
- Modify: `client/main.go`

- [ ] **Step 1: Write the updated WASM bridge code**

```go
//go:build js && wasm

package main

import (
	"context"
	"encoding/json"
	"syscall/js"

	"github.com/samcharles93/NellDB"
	"github.com/samcharles93/NellDB/sdk"
)

var (
	store = nell.NewMemoryStore("wasm-client")
	db    = sdk.New(store, "wasm-client")
	ctx   = context.Background()
)

func registerCallbacks() {
	// ── Write ──────────────────────────────────────────────────────────────
	js.Global().Set("nellPut", js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) == 0 {
			return js.ValueOf(errorJSON("missing argument"))
		}
		var doc sdk.Doc
		if err := json.Unmarshal([]byte(args[0].String()), &doc); err != nil {
			return js.ValueOf(errorJSON(err.Error()))
		}
		rev, err := db.Put(ctx, doc)
		if err != nil {
			return js.ValueOf(errorJSON(err.Error()))
		}
		resp, _ := json.Marshal(map[string]any{
			"ok":  true,
			"rev": rev,
		})
		return js.ValueOf(string(resp))
	}))

	// ── Read ──────────────────────────────────────────────────────────────
	js.Global().Set("nellGet", js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) == 0 {
			return js.ValueOf(errorJSON("missing id"))
		}
		doc, err := db.Get(ctx, args[0].String())
		if err != nil {
			return js.ValueOf(errorJSON(err.Error()))
		}
		resp, _ := json.Marshal(map[string]any{"ok": true, "doc": doc})
		return js.ValueOf(string(resp))
	}))

	// ── Remove ────────────────────────────────────────────────────────────
	js.Global().Set("nellRemove", js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) == 0 {
			return js.ValueOf(errorJSON("missing argument"))
		}
		
		var idOrDoc any
		argStr := args[0].String()
		if argStr != "" && argStr[0] == '{' {
			var doc sdk.Doc
			if err := json.Unmarshal([]byte(argStr), &doc); err == nil {
				idOrDoc = doc
			} else {
				idOrDoc = argStr 
			}
		} else {
			idOrDoc = argStr
		}
		
		rev, err := db.Remove(ctx, idOrDoc)
		if err != nil {
			return js.ValueOf(errorJSON(err.Error()))
		}
		resp, _ := json.Marshal(map[string]any{"ok": true, "rev": rev})
		return js.ValueOf(string(resp))
	}))

	// ── AllDocs ───────────────────────────────────────────────────────────
	js.Global().Set("nellAllDocs", js.FuncOf(func(this js.Value, args []js.Value) any {
		var rng sdk.DocRange
		if len(args) > 0 && args[0].Type() == js.TypeString && args[0].String() != "" {
			if err := json.Unmarshal([]byte(args[0].String()), &rng); err != nil {
				return js.ValueOf(errorJSON(err.Error()))
			}
		}
		res, err := db.AllDocs(ctx, rng)
		if err != nil {
			return js.ValueOf(errorJSON(err.Error()))
		}
		resp, _ := json.Marshal(map[string]any{"ok": true, "result": res})
		return js.ValueOf(string(resp))
	}))

	// ── Sync ──────────────────────────────────────────────────────────────
	js.Global().Set("nellSync", js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) == 0 {
			return js.ValueOf(errorJSON("missing serverUrl"))
		}
		serverUrl := args[0].String()
		
		// Wrap async work in a JS Promise since sync hits the network
		promiseConstructor := js.Global().Get("Promise")
		return promiseConstructor.New(js.FuncOf(func(this js.Value, pArgs []js.Value) any {
			resolve := pArgs[0]
			reject := pArgs[1]

			go func() {
				rep := sdk.NewReplicator(db, serverUrl)
				pushed, pulled, err := rep.Sync(ctx)
				if err != nil {
					reject.Invoke(js.Global().Get("Error").New(err.Error()))
					return
				}
				resp, _ := json.Marshal(map[string]any{
					"ok":     true,
					"pushed": pushed,
					"pulled": pulled,
				})
				resolve.Invoke(js.ValueOf(string(resp)))
			}()
			return nil
		}))
	}))

	js.Global().Set("nellReady", js.ValueOf(true))
}

func errorJSON(msg string) string {
	b, _ := json.Marshal(map[string]any{"ok": false, "error": msg})
	return string(b)
}

func main() {
	ch := make(chan struct{}, 0)
	registerCallbacks()
	<-ch
}
```

- [ ] **Step 2: Commit changes to main.go**

```bash
git add client/main.go
git commit -m "feat: expose sdk.DocDB semantics in WASM exports"
```

### Task 2: Update the JavaScript SDK API

**Files:**
- Modify: `client/nell.js`

- [ ] **Step 1: Write the updated JS API code**

```javascript
/**
 * NellDB — JavaScript SDK
 *
 * Distributed, real-time, offline-first document database.
 * One import, one init call, then full CRUD + sync.
 *
 * @example
 *   import { NellDB } from '@nelldb/sdk';
 *   const db = new NellDB();
 *   await db.init();
 *   await db.put({ _id: 'note-1', title: 'Hello' });
 *   await db.replicate.to('https://home.example.com');
 */
const isNode = typeof window === 'undefined';
const globalScope = isNode ? global : window;

if (!globalScope.Go && !isNode) {
    // wasm_exec.js must be loaded before this file in browser contexts.
    // In Node, require it dynamically.
}

class NellDB {
    constructor() {
        this.go = new globalScope.Go();
        this.instance = null;
    }

    // ── Lifecycle ─────────────────────────────────────────────────────────

    /**
     * Load the WASM engine.  Must be called once before any operations.
     * @param {string|ArrayBuffer|Uint8Array} wasmUrlOrBuffer
     */
    async init(wasmUrlOrBuffer) {
        if (this.instance) return;

        if (isNode || wasmUrlOrBuffer instanceof ArrayBuffer || wasmUrlOrBuffer instanceof Uint8Array) {
            const result = await WebAssembly.instantiate(wasmUrlOrBuffer, this.go.importObject);
            this.instance = result.instance;
        } else {
            const result = await WebAssembly.instantiateStreaming(fetch(wasmUrlOrBuffer), this.go.importObject);
            this.instance = result.instance;
        }

        this.go.run(this.instance);
    }

    // ── CRUD ──────────────────────────────────────────────────────────────

    /**
     * Insert or update a document.
     * @param {object} doc - Document object, requires an _id.
     * @returns {{ok:boolean, rev:string}}
     */
    put(doc) {
        this._requireReady();
        const resp = JSON.parse(globalScope.nellPut(JSON.stringify(doc)));
        if (!resp.ok) throw new Error(resp.error);
        // Note: the SDK docdb mutates the passed in doc with _rev in Go,
        // we reflect that here if we want or just return the rev.
        doc._rev = resp.rev;
        return resp;
    }

    /**
     * Fetch a single document by ID.
     * @param {string} id
     * @returns {object} The document.
     */
    get(id) {
        this._requireReady();
        const resp = JSON.parse(globalScope.nellGet(id));
        if (!resp.ok) throw new Error(resp.error);
        return resp.doc;
    }

    /**
     * Tombstone a document.
     * @param {string|object} idOrDoc
     * @returns {{ok:boolean, rev:string}}
     */
    remove(idOrDoc) {
        this._requireReady();
        const arg = typeof idOrDoc === 'string' ? idOrDoc : JSON.stringify(idOrDoc);
        const resp = JSON.parse(globalScope.nellRemove(arg));
        if (!resp.ok) throw new Error(resp.error);
        return resp;
    }

    /**
     * List all non-deleted documents.
     * @param {object} options
     * @returns {object} AllDocsResult
     */
    allDocs(options = {}) {
        this._requireReady();
        const resp = JSON.parse(globalScope.nellAllDocs(JSON.stringify(options)));
        if (!resp.ok) throw new Error(resp.error);
        return resp.result;
    }

    // ── Sync ──────────────────────────────────────────────────────────────

    /**
     * Connect to a home server and begin syncing.
     * @param {string} serverUrl - e.g. "https://home.example.com"
     * @returns {Promise<{ok:boolean, pushed:number, pulled:number}>}
     */
    async sync(serverUrl) {
        this._requireReady();
        this._serverUrl = serverUrl;
        
        const respRaw = await globalScope.nellSync(serverUrl);
        const resp = JSON.parse(respRaw);
        if (!resp.ok) throw new Error(resp.error);

        if (this._onSyncComplete) this._onSyncComplete();
        return resp;
    }

    // ── Lifecycle hooks ───────────────────────────────────────────────────

    /** @param {() => void} cb */
    onConnect(cb) { this._onConnect = cb; }

    /** @param {() => void} cb */
    onDisconnect(cb) { this._onDisconnect = cb; }

    /** @param {(id:string, local:object, accepted:object) => void} cb */
    onConflict(cb) { this._onConflict = cb; }

    /** @param {() => void} cb */
    onSyncComplete(cb) { this._onSyncComplete = cb; }

    // ── Internal ──────────────────────────────────────────────────────────

    _requireReady() {
        if (!globalScope.nellPut) {
            throw new Error('Nell WASM engine not initialised. Call await db.init() first.');
        }
    }
}

module.exports = { NellDB };
```

- [ ] **Step 2: Commit changes to nell.js**

```bash
git add client/nell.js
git commit -m "feat: expose document semantics in JS SDK"
```

### Task 3: Update `client/wasm_test.go` integration test

**Files:**
- Modify: `client/wasm_test.go`

- [ ] **Step 1: Write the updated test assertions**

```go
package client

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

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

func buildWASMClient(t *testing.T, root string) string {
	t.Helper()

	wasmPath := filepath.Join(t.TempDir(), "nell-test.wasm")
	cmd := exec.Command("go", "build", "-o", wasmPath, "./client/main.go")
	cmd.Dir = root
	cmd.Env = append(os.Environ(),
		"GOOS=js",
		"GOARCH=wasm",
		"CGO_ENABLED=0",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build WASM client:\n%s", out)
	}
	return wasmPath
}

func writeNodeHarness(t *testing.T) string {
	t.Helper()

	scriptPath := filepath.Join(t.TempDir(), "wasm_client_harness.js")
	if err := os.WriteFile(scriptPath, []byte(nodeHarness), 0o644); err != nil {
		t.Fatalf("write Node harness: %v", err)
	}
	return scriptPath
}

func repoRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Dir(dir)
}

func wasmExecPath(t *testing.T) string {
	t.Helper()

	goroot := strings.TrimSpace(goEnv(t, "GOROOT"))
	path := filepath.Join(goroot, "lib", "wasm", "wasm_exec.js")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("wasm_exec.js not found at %s: %v", path, err)
	}
	return path
}

func goEnv(t *testing.T, key string) string {
	t.Helper()

	cmd := exec.Command("go", "env", key)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go env %s: %v\n%s", key, err, out)
	}
	return string(out)
}

const nodeHarness = `
const fs = require("fs");
const path = require("path");
const util = require("util");
const crypto = require("crypto");
const { performance } = require("perf_hooks");

async function main() {
  const [, , wasmExecPath, wasmPath] = process.argv;
  if (!wasmExecPath || !wasmPath) {
    throw new Error("usage: node harness.js <wasm_exec.js> <binary.wasm>");
  }

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
  go.exit = (code) => {
    if (code !== 0) {
      throw new Error("Go runtime exited with code " + code);
    }
  };

  const result = await WebAssembly.instantiate(fs.readFileSync(wasmPath), go.importObject);
  const runPromise = go.run(result.instance);
  runPromise.catch((err) => {
    console.error(err);
    process.exit(1);
  });

  const deadline = Date.now() + 2000;
  while (!globalThis.nellReady && Date.now() < deadline) {
    await new Promise((resolve) => setTimeout(resolve, 10));
  }
  if (!globalThis.nellReady) {
    throw new Error("nellReady was never set");
  }

  const putResp = JSON.parse(globalThis.nellPut(JSON.stringify({
    _id: "note:1",
    title: "hello from wasm"
  })));
  if (!putResp.ok || !putResp.rev) {
    throw new Error("unexpected put response: " + JSON.stringify(putResp));
  }

  const getResp = JSON.parse(globalThis.nellGet("note:1"));
  if (!getResp.ok || getResp.doc.title !== "hello from wasm" || !getResp.doc._rev) {
    throw new Error("unexpected get response: " + JSON.stringify(getResp));
  }

  const listResp = JSON.parse(globalThis.nellAllDocs("{}"));
  if (!listResp.ok || listResp.result.total_rows !== 1 || listResp.result.rows[0].id !== "note:1") {
    throw new Error("unexpected list response: " + JSON.stringify(listResp));
  }

  const deleteResp = JSON.parse(globalThis.nellRemove("note:1"));
  if (!deleteResp.ok || !deleteResp.rev) {
    throw new Error("unexpected delete response: " + JSON.stringify(deleteResp));
  }

  const emptyListResp = JSON.parse(globalThis.nellAllDocs("{}"));
  if (!emptyListResp.ok || emptyListResp.result.total_rows !== 0) {
    throw new Error("unexpected list after delete: " + JSON.stringify(emptyListResp));
  }

  const invalidJSONResp = JSON.parse(globalThis.nellPut("{not-json"));
  if (invalidJSONResp.ok || !invalidJSONResp.error) {
    throw new Error("expected invalid JSON error, got: " + JSON.stringify(invalidJSONResp));
  }

  const missingIDResp = JSON.parse(globalThis.nellGet(""));
  if (missingIDResp.ok || !missingIDResp.error) {
    throw new Error("expected error, got: " + JSON.stringify(missingIDResp));
  }

  process.exit(0);
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
`
```

- [ ] **Step 2: Run the test to verify changes pass**

Run: `go test -v ./client`

- [ ] **Step 3: Commit test updates**

```bash
git add client/wasm_test.go
git commit -m "test: update wasm node harness to test doc semantics"
```

---

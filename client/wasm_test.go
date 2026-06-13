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
	fakeIDBPath := fakeIDBAutoPath(t, root)

	cmd := exec.Command("node", scriptPath, wasmExecPath, wasmPath, fakeIDBPath)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run WASM harness:\n%s", out)
	}
}

// TestWASMPersistentNodeID verifies that the WASM client's persistent
// nodeID (and any data written to IndexedDBStore) survives a reload of the
// Go runtime.  Loads the same WASM twice in the same Node process, with
// fake-indexeddb as the backing store, and asserts the nodeID matches and
// the document written in the first session is still readable in the second.
func TestWASMPersistentNodeID(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not in PATH; skipping WASM integration test")
	}

	root := repoRoot(t)
	wasmPath := buildWASMClient(t, root)
	scriptPath := writeNodeReloadHarness(t)
	wasmExecPath := wasmExecPath(t)
	fakeIDBPath := fakeIDBAutoPath(t, root)

	cmd := exec.Command("node", scriptPath, wasmExecPath, wasmPath, fakeIDBPath)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run WASM reload harness:\n%s", out)
	}
}

func writeNodeHarness(t *testing.T) string {
	t.Helper()

	scriptPath := filepath.Join(t.TempDir(), "wasm_client_harness.js")
	if err := os.WriteFile(scriptPath, []byte(nodeHarness), 0o644); err != nil {
		t.Fatalf("write Node harness: %v", err)
	}
	return scriptPath
}

func writeNodeReloadHarness(t *testing.T) string {
	t.Helper()

	scriptPath := filepath.Join(t.TempDir(), "wasm_reload_harness.js")
	if err := os.WriteFile(scriptPath, []byte(nodeReloadHarness), 0o644); err != nil {
		t.Fatalf("write Node reload harness: %v", err)
	}
	return scriptPath
}

func fakeIDBAutoPath(t *testing.T, root string) string {
	t.Helper()

	// fake-indexeddb v6 ships the auto-polyfill entry at
	// `fake-indexeddb/auto/index.js`.  Older versions shipped a flat
	// `auto.js`; check both for compatibility.
	candidates := []string{
		filepath.Join(root, "node_modules", "fake-indexeddb", "auto", "index.js"),
		filepath.Join(root, "node_modules", "fake-indexeddb", "auto.js"),
	}
	for _, path := range candidates {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	t.Skipf("fake-indexeddb not installed; run `npm install` at repo root (looked in %v)", candidates)
	return ""
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
  const [, , wasmExecPath, wasmPath, fakeIDBPath] = process.argv;
  if (!wasmExecPath || !wasmPath) {
    throw new Error("usage: node harness.js <wasm_exec.js> <binary.wasm> [fake-indexeddb-auto.js]");
  }

  globalThis.require = require;
  globalThis.fs = fs;
  globalThis.path = path;
  globalThis.TextEncoder = util.TextEncoder;
  globalThis.TextDecoder = util.TextDecoder;
  globalThis.performance ??= performance;
  globalThis.crypto ??= crypto.webcrypto ?? crypto;

  // Polyfill IndexedDB so the WASM client exercises the real
  // IndexedDBStore path instead of falling back to MemoryStore.
  if (fakeIDBPath) {
    require(fakeIDBPath);
  }

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

  const putResp = JSON.parse(await globalThis.nellPut(JSON.stringify({
    _id: "note:1",
    title: "hello from wasm"
  })));
  if (!putResp.ok || !putResp.rev) {
    throw new Error("unexpected put response: " + JSON.stringify(putResp));
  }

  const getResp = JSON.parse(await globalThis.nellGet("note:1"));
  if (!getResp.ok || getResp.doc.title !== "hello from wasm" || !getResp.doc._rev) {
    throw new Error("unexpected get response: " + JSON.stringify(getResp));
  }

  const listResp = JSON.parse(await globalThis.nellAllDocs("{}"));
  const userRows = listResp.result.rows.filter(r => !r.id.startsWith("meta:"));
  if (!listResp.ok || userRows.length !== 1 || userRows[0].id !== "note:1") {
    throw new Error("unexpected list response: " + JSON.stringify(listResp));
  }

  const deleteResp = JSON.parse(await globalThis.nellRemove("note:1"));
  if (!deleteResp.ok || !deleteResp.rev) {
    throw new Error("unexpected delete response: " + JSON.stringify(deleteResp));
  }

  const emptyListResp = JSON.parse(await globalThis.nellAllDocs("{}"));
  const emptyUserRows = emptyListResp.result.rows.filter(r => !r.id.startsWith("meta:"));
  if (!emptyListResp.ok || emptyUserRows.length !== 0) {
    throw new Error("unexpected list after delete: " + JSON.stringify(emptyListResp));
  }

  const invalidJSONResp = JSON.parse(await globalThis.nellPut("{not-json"));
  if (invalidJSONResp.ok || !invalidJSONResp.error) {
    throw new Error("expected invalid JSON error, got: " + JSON.stringify(invalidJSONResp));
  }

  const missingIDResp = JSON.parse(await globalThis.nellGet(""));
  if (missingIDResp.ok || !missingIDResp.error) {
    throw new Error("expected error, got: " + JSON.stringify(missingIDResp));
  }

  // Sanity: the nodeID should be a non-empty UUID-shaped string.
  const nodeID = globalThis.nellNodeID();
  if (typeof nodeID !== "string" || !/^[0-9a-f-]{36}$/.test(nodeID)) {
    throw new Error("nellNodeID is not a UUID: " + JSON.stringify(nodeID));
  }

  process.exit(0);
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
`

// nodeReloadHarness loads the WASM twice in the same Node process and
// asserts that the persistent nodeID and any data written in the first
// session are still visible in the second.
const nodeReloadHarness = `
const fs = require("fs");
const path = require("path");
const util = require("util");
const crypto = require("crypto");
const { performance } = require("perf_hooks");

async function loadWasm(wasmPath, wasmExecPath) {
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
  go.run(result.instance);
  const deadline = Date.now() + 2000;
  while (!globalThis.nellReady && Date.now() < deadline) {
    await new Promise((resolve) => setTimeout(resolve, 10));
  }
  if (!globalThis.nellReady) {
    throw new Error("nellReady was never set");
  }
  return go;
}

function resetNellGlobals() {
  // Drop the bindings from the previous WASM so the next load re-registers them.
  for (const k of ["nellReady", "nellPut", "nellGet", "nellRemove", "nellAllDocs", "nellSync", "nellNodeID"]) {
    try { delete globalThis[k]; } catch (_) { globalThis[k] = undefined; }
  }
}

async function main() {
  const [, , wasmExecPath, wasmPath, fakeIDBPath] = process.argv;
  if (!wasmExecPath || !wasmPath) {
    throw new Error("usage: node reload-harness.js <wasm_exec.js> <binary.wasm> <fake-indexeddb-auto.js>");
  }
  if (!fakeIDBPath) {
    throw new Error("fake-indexeddb path is required for the reload harness");
  }

  globalThis.require = require;
  globalThis.fs = fs;
  globalThis.path = path;
  globalThis.TextEncoder = util.TextEncoder;
  globalThis.TextDecoder = util.TextDecoder;
  globalThis.performance ??= performance;
  globalThis.crypto ??= crypto.webcrypto ?? crypto;

  // Polyfill IndexedDB once; fake-indexeddb persists in the Node global
  // across WASM instantiations, so the second load will see what the
  // first one wrote.
  require(fakeIDBPath);

  // ── First session: open store, generate nodeID, write a doc ──
  await loadWasm(wasmPath, wasmExecPath);
  const nodeID1 = globalThis.nellNodeID();
  if (typeof nodeID1 !== "string" || !/^[0-9a-f-]{36}$/.test(nodeID1)) {
    throw new Error("first nodeID is not a UUID: " + JSON.stringify(nodeID1));
  }
  const putResp = JSON.parse(await globalThis.nellPut(JSON.stringify({
    _id: "persistence-test",
    body: "from the first session"
  })));
  if (!putResp.ok) {
    throw new Error("first put failed: " + JSON.stringify(putResp));
  }
  resetNellGlobals();

  // ── Second session: same Node process, same fake-indexeddb, fresh WASM ──
  await loadWasm(wasmPath, wasmExecPath);
  const nodeID2 = globalThis.nellNodeID();
  if (nodeID1 !== nodeID2) {
    throw new Error("nodeID changed across reloads: " + nodeID1 + " vs " + nodeID2);
  }

  const getResp = JSON.parse(await globalThis.nellGet("persistence-test"));
  if (!getResp.ok || getResp.doc.body !== "from the first session") {
    throw new Error("doc lost across reloads: " + JSON.stringify(getResp));
  }

  // AllDocs should still list the doc.
  const listResp = JSON.parse(await globalThis.nellAllDocs("{}"));
  const userRows = (listResp.result.rows || []).filter(r => !r.id.startsWith("meta:"));
  if (!listResp.ok || userRows.length !== 1 || userRows[0].id !== "persistence-test") {
    throw new Error("AllDocs lost the doc across reloads: " + JSON.stringify(listResp));
  }

  process.exit(0);
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
`

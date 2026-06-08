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
  const userRows = listResp.result.rows.filter(r => !r.id.startsWith("meta:"));
  if (!listResp.ok || userRows.length !== 1 || userRows[0].id !== "note:1") {
    throw new Error("unexpected list response: " + JSON.stringify(listResp));
  }

  const deleteResp = JSON.parse(globalThis.nellRemove("note:1"));
  if (!deleteResp.ok || !deleteResp.rev) {
    throw new Error("unexpected delete response: " + JSON.stringify(deleteResp));
  }

  const emptyListResp = JSON.parse(globalThis.nellAllDocs("{}"));
  const emptyUserRows = emptyListResp.result.rows.filter(r => !r.id.startsWith("meta:"));
  if (!emptyListResp.ok || emptyUserRows.length !== 0) {
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

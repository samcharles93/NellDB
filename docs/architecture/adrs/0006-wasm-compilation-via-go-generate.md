# ADR 0006: WASM Compilation via go:generate

- **Status:** Accepted
- **Date:** 2026-06-07

## Context

The client is Go code that must compile to WASM for browser/Electron/Capacitor runtimes. Producing the WASM binary typically requires:
1. A shell script or Makefile with the right `GOOS`/`GOARCH` env vars.
2. Copying `wasm_exec.js` from the Go installation directory for the browser runtime shim.

This fragments the build tooling — developers need to remember which script to run, and CI must be configured for both Go and shell steps.

The Go toolchain already has `go generate` for exactly this: embedding code-generation steps inside the Go build system.

## Decision

Embed all WASM compilation steps in `client/generate.go` using `//go:generate` directives:

```go
package client

//go:generate env GOOS=js GOARCH=wasm go build -ldflags="-s -w" -o nell.wasm main.go
//go:generate cp $GOROOT/misc/wasm/wasm_exec.js .
```

Running `go generate ./client/...` produces:
- `nell.wasm` — the compiled engine binary.
- `wasm_exec.js` — Go's WebAssembly execution shim.

The output combines with the hand-written `nell.js` SDK wrapper to form a self-contained client bundle.

## Consequences

### Positive

- Single Go toolchain command produces the client bundle — no Makefiles, no npm scripts.
- `wasm_exec.js` is always copied from the active Go installation, ensuring version compatibility.
- The `-ldflags="-s -w"` flag strips debug info, producing a smaller binary (~2-5MB typical for Go WASM).
- `go generate ./...` can be used at the root to build everything.

### Negative

- Developers must remember to run `go generate` after modifying `core/` or `client/` before testing frontend changes.
- `go generate` only works when the Go toolchain is installed (not relevant for npm-only workflows).
- The Makefile duplicates the build logic for CI convenience — the two must stay in sync.

### Mitigations

- Add a `//go:generate` directory walk at the root level so `go generate ./...` catches everything.
- The Makefile's `build-wasm` target duplicates the commands as a convenience for `make`-oriented developers; keep them in sync.

---

**Implementation:** `client/generate.go`

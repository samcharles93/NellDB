package client

// Command to build the WASM binary target
//go:generate env GOOS=js GOARCH=wasm go build -ldflags="-s -w" -o nell.wasm main.go

// Command to pull the correct runtime shim from your active Go installation.
//go:generate cp $GOROOT/lib/wasm/wasm_exec.js .

.PHONY: build-wasm build-server test-wasm clean

build-wasm:
	@echo "Building WASM binary..."
	GOOS=js GOARCH=wasm go build -o client/nell.wasm client/main.go
	cp "$$(go env GOROOT)/lib/wasm/wasm_exec.js" client/

build-server:
	@echo "Building native server..."
	go build -o bin/nelldb-server ./cmd/nelldb-server/

test-wasm:
	@echo "Running WASM integration tests..."
	go test ./client -run '^TestWASM'

clean:
	rm -f client/nell.wasm client/wasm_exec.js
	rm -rf bin/

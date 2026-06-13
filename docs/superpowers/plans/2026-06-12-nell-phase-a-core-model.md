# Nell Core Overhaul Phase A: Data Model & Protobuf Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Refactor the core `Record` struct to support collections and implement a Protobuf-based binary wire format.

**Architecture:** Update `nell.Record` to include `Collection` and use `string` for `Type` to align with Protobuf enums/constants. Introduce a `.proto` schema and generated Go code to replace JSON for sync payloads.

**Tech Stack:** Go, Protobuf (protoc-gen-go), `google.golang.org/protobuf`.

---

## File Changes Map

- **Modify:** `types.go` - Update `Record` and `DataType` definitions.
- **Modify:** `store.go` - Update `Store` interface signatures to be collection-aware.
- **Create:** `nell.proto` - Define the binary schema.
- **Create:** `pb/nell.pb.go` - Generated protobuf code.
- **Modify:** `server/main.go` - Update handlers to use Protobuf instead of JSON.

---

### Task 1: Update Core Types and Interface

**Files:**
- Modify: `types.go`
- Modify: `store.go`
- Test: `store_test.go`

- [ ] **Step 1: Update Record struct in types.go**

```go
// types.go
const DefaultCollection = "default"

type Record struct {
    Collection string    `json:"collection"`
    ID         string    `json:"id"`
    Type       string    `json:"type"` // Changed from DataType to string for proto compatibility
    Payload    []byte    `json:"payload,omitempty"`
    Vector     []float32 `json:"vector,omitempty"`
    Clock      HLC       `json:"clock"`
    UpdatedBy  string    `json:"updated_by"`
    Deleted    bool      `json:"deleted"`
}
```

- [ ] **Step 2: Update Store interface in store.go**

```go
// store.go
type Store interface {
    Put(incoming Record) (accepted bool, current Record, err error)
    Delete(collection, id string) (Record, error)
    Get(collection, id string) (Record, error)
    List(collection string) ([]Record, error)
    Query(q Query) ([]Record, error)
    GetChangesSince(since HLC) ([]Record, error)
    NodeID() string
    Close() error
}
```

- [ ] **Step 3: Run existing tests and verify breakage**

Run: `go test ./...`
Expected: Compilation errors due to interface mismatch.

- [ ] **Step 4: Fix MemoryStore implementation to match new interface**

Update `MemoryStore` methods in `store.go` to handle the `collection` parameter (use a composite key `col + ":" + id` or nested maps).

- [ ] **Step 5: Verify tests pass**

Run: `go test ./...`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add types.go store.go
git commit -m "refactor: make Record and Store collection-aware"
```

---

### Task 2: Implement Protobuf Schema

**Files:**
- Create: `nell.proto`
- Create: `Makefile` (update)

- [ ] **Step 1: Create nell.proto**

```protobuf
syntax = "proto3";
package nell;
option go_package = "github.com/samcharles93/NellDB/pb";

message HLC {
  int64 wall_time = 1;
  int32 counter = 2;
}

message Record {
  string collection = 1;
  string id = 2;
  string type = 3;
  bytes payload = 4;
  repeated float32 vector = 5;
  HLC clock = 6;
  string updated_by = 7;
  bool deleted = 8;
}

message SyncBatch {
  repeated Record changes = 1;
  map<string, HLC> knowledge_vector = 2;
  string subscription_id = 3;
}
```

- [ ] **Step 2: Add generate target to Makefile**

```makefile
proto:
	protoc --go_out=. --go_opt=paths=source_relative nell.proto
```

- [ ] **Step 3: Generate Go code**

Run: `make proto`
Expected: `pb/nell.pb.go` is created.

- [ ] **Step 4: Commit**

```bash
git add nell.proto Makefile
git commit -m "feat: add protobuf schema for sync"
```

---

### Task 3: Migrate Server to Protobuf

**Files:**
- Modify: `server/main.go`

- [ ] **Step 1: Update handlePush to use Proto decoding**

```go
// server/main.go snippet
func (s *Server) handlePush(w http.ResponseWriter, r *http.Request) {
    body, _ := io.ReadAll(r.Body)
    var batch pb.SyncBatch
    if err := proto.Unmarshal(body, &batch); err != nil {
        http.Error(w, "invalid proto", 400)
        return
    }
    // ... map pb.Record to nell.Record and apply
}
```

- [ ] **Step 2: Update handlePull to use Proto encoding**

Set `Content-Type: application/x-protobuf` and use `proto.Marshal`.

- [ ] **Step 3: Verify with integration test**

Create a temporary test in `server/server_test.go` that sends a `SyncBatch` and verifies acceptance.

- [ ] **Step 4: Commit**

```bash
git add server/main.go
git commit -m "feat: switch server sync to protobuf"
```

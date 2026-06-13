# ADR 0001: Unified Multi-Modal Data Model

- **Status:** Accepted
- **Date:** 2026-06-07

## Context

Nell-engine must handle text strings, raw image binaries, and floating-point vector embeddings. These three modalities have fundamentally different characteristics:

- **Text:** variable-length UTF-8 strings, typically KBs in size.
- **Images:** binary blobs from KBs to MBs, no useful semantics at the byte level.
- **Vectors:** fixed-dimension `[]float32` arrays (e.g. 384, 768, 1536 dimensions), queried via similarity not equality.

On the client (WASM in browser/Electron), memory is constrained. On the server, the store should be fast to iterate for sync queries. A separate table-per-type design would force the sync engine to reconcile multiple schemas, increasing complexity.

The engine also needs tombstones for deletion propagation in a multi-primary system.

## Decision

We use a single flat `Record` struct that accommodates all three types via a discriminator and two payload fields:

```go
type DataType string

const (
    TypeText   DataType = "text"
    TypeVector DataType = "vector"
    TypeImage  DataType = "image"
)

type HLC struct {
    WallTime int64 `json:"wall_time"`
    Counter  int32 `json:"counter"`
}

type Record struct {
    ID        string    `json:"id"`
    Type      DataType  `json:"type"`
    Payload   []byte    `json:"payload,omitempty"`
    Vector    []float32 `json:"vector,omitempty"`
    Clock     HLC       `json:"clock"`
    UpdatedBy string    `json:"updated_by"`
    Deleted   bool      `json:"deleted"`
}
```

- **`Payload []byte`** carries text and image data. The application layer encodes/decodes as needed.
- **`Vector []float32`** is a separate field so similarity searches can scan vectors without unmarshalling the Payload.
- **`Clock HLC`** provides causal ordering (see ADR 0002).
- **`UpdatedBy string`** identifies the node that last mutated the record (used in conflict resolution, see ADR 0003).
- **`Deleted bool`** is an explicit tombstone for deletion propagation.
- The `DataType` discriminator is not exhaustive — new types can be added without structural changes.

## Consequences

### Positive

- Single serialisation path for the sync engine — one wire format, one merge function.
- Minimal overhead: no schema registry, no type-specific tables.
- Vector scans don't touch Payload bytes.
- The struct is JSON-serialisable out of the box, making the WASM bridge trivial.

### Negative

- Application layer must handle encoding/decoding text or image formats from the byte slice.
- No schema enforcement — malformed payloads are caught at the application level, not the engine level.

### Mitigations

- Document the expected encoding conventions (e.g. text is UTF-8, images are the original file bytes).
- Consider a `ContentType` metadata field in future iterations for automatic codec selection.

---

**Implementation:** `core/types.go`

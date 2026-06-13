# ADR 0003: Last-Write-Wins Conflict Resolution

- **Status:** Accepted
- **Date:** 2026-06-07

## Context

In a multi-primary system where clients can be offline for extended periods, concurrent edits to the same document are inevitable. When two nodes independently modify the same record and then sync, the system must resolve the conflict.

Options considered:
- **Multi-version (CouchDB model):** Keep all conflicting branches and let the application resolve. Powerful but complex — requires DAG management, compaction, and application-level merge callbacks.
- **CRDTs:** Mergeable data types with strong eventual consistency guarantees. Mathematically elegant but add complexity per data type (text CRDT != image CRDT).
- **Last-Write-Wins (LWW):** Accept one version, discard the other. Simple, fast, deterministic.

For a PoC targeting docs, notes, and Obsidian-like use cases, LWW is the pragmatic choice. Most conflicts in this domain are accidental (e.g. a sync race), not intentional concurrent edits. When they are intentional, the user expects the most recent save to win.

## Decision

Resolve all write conflicts using Last-Write-Wins (LWW) with HLC ordering and deterministic tie-breaking:

```go
func ResolveConflict(local, incoming *Record) *Record {
    if incoming.Clock.GreaterThan(local.Clock) {
        return incoming
    }
    if local.Clock.GreaterThan(incoming.Clock) {
        return local
    }
    // Tie: deterministic lexical comparison of node IDs
    if incoming.UpdatedBy > local.UpdatedBy {
        return incoming
    }
    return local
}
```

The resolution is:
1. The record with the higher HLC clock wins.
2. If clocks are equal (concurrent events in the same millisecond), the record whose `UpdatedBy` string is lexically greater wins.

The same logic is applied everywhere — client, server, and during replication. Because the rules are deterministic and data converges, every node reaches the same conclusion given the same inputs.

Deletions are also resolved via LWW: a tombstone (`Deleted: true`) with a higher clock overwrites a live record, and vice versa.

## Consequences

### Positive

- Extremely fast — a few integer comparisons and string compares.
- Completely decentralised — no coordinator or lock needed.
- Converges to identical state across all nodes given the same inputs.
- Trivial to reason about and test.

### Negative

- Intentional concurrent edits to the same record see one silently overwrite the other.
- No conflict visibility — the losing edit is discarded without notification (in the PoC; a notification mechanism can be added later).
- Lexical tie-breaker is arbitrary and unrelated to actual application semantics.

### Mitigations

- Expose conflict events in the SDK so applications can detect overwrites if needed.
- For a PoC, LWW is sufficient. A future iteration can layer conflict branches on top of the same clock infrastructure.

---

**Implementation:** `core/store.go` — `MemoryStore.Put()` embeds the LWW logic inline.

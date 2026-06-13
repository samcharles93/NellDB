# ADR 0180: Define a strict schema registry as a versioned flatbuffer schema file compile...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** core
- **Proposed by:** enterprise_pragmatist-04
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Define a strict schema registry as a versioned flatbuffer schema file compiled into the WASM binary, where every mutation payload must pass a zero-copy validation pass (bounds checks, required fields, enum ranges, vector dimension matching) before entering the WAL — invalid mutations are rejected at ingestion with a typed error code logged to an immutable SchemaViolation record, preventing corrupt state from ever reaching the materialized view. Each transaction batch commits atomically only after all mutations pass validation, the post-apply state hash matches a deterministic recomputation, and a SchemaVersion anchor is written to the TransactionCommit record, enabling point-in-time schema compliance audits and safe rolling upgrades without migration scripts.

## Consequences

*To be determined as the architecture is implemented.*

---

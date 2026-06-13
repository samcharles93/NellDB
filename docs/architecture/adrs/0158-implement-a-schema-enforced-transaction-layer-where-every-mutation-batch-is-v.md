# ADR 0158: Implement a schema-enforced transaction layer where every mutation batch is v...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** core
- **Proposed by:** enterprise_pragmatist-04
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Implement a schema-enforced transaction layer where every mutation batch is validated against a registered JSON Schema (stored in a versioned schema registry with content-addressable IDs) before WAL append; each transaction carries a Merkle proof of its causal dependencies and a deterministic state hash (BLAKE3 of post-apply materialized view), enabling point-in-time verification and strict schema constraints without runtime reflection. Transactions that fail schema validation or causal proof verification are rejected at ingress with a structured error code, never entering the WAL.

## Consequences

*To be determined as the architecture is implemented.*

---

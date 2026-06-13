# ADR 0029: Implement offline transactions as signed intent logs: each transaction record...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** client
- **Proposed by:** enterprise_pragmatist-09
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Implement offline transactions as signed intent logs: each transaction records its read-set (keys + expected HLC versions) and write-set (mutations + schema version) in a single CBOR envelope signed by the client's Ed25519 key; at sync, the server atomically validates all preconditions against current state before applying any writes, rejecting the entire transaction as a structured conflict object (with DVV causal context) if any version mismatch occurs — no partial commits, no silent overwrites, full audit trail.

## Consequences

*To be determined as the architecture is implemented.*

---

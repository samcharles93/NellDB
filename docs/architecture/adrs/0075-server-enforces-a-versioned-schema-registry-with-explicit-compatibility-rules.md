# ADR 0075: Server enforces a versioned schema registry with explicit compatibility rules...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** server
- **Proposed by:** enterprise_pragmatist-04
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Server enforces a versioned schema registry with explicit compatibility rules (BACKWARD, FORWARD, FULL) and requires every sync batch to declare its schema version; mutations failing validation against the registered schema are rejected atomically with a structured error code. Each committed transaction emits a deterministic audit entry to an immutable audit log keyed by (collection, schema_version, transaction_id) containing the full before/after state hashes, HLC range, and conflict resolution trace — clients MUST verify the audit log's merkle root against their local state during sync, making silent schema drift or data loss cryptographically impossible.

## Consequences

*To be determined as the architecture is implemented.*

---

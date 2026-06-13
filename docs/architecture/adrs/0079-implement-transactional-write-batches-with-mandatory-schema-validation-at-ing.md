# ADR 0079: Implement transactional write batches with mandatory schema validation at ing...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** server
- **Proposed by:** enterprise_pragmatist-04
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Implement transactional write batches with mandatory schema validation at ingest: each sync batch declares a schema version hash and carries a batch-scoped transaction ID; the server validates all mutations against the registered schema before applying HLC-ordered LWW resolution, and atomically commits or rejects the entire batch. Every resolved conflict emits a structured audit entry (losing payload hash, winning HLC, schema version, transaction ID) to an immutable append-only audit log that clients can verify via merkle proofs during sync, preventing silent schema drift and enabling forensic replay of any document's conflict history.

## Consequences

*To be determined as the architecture is implemented.*

---

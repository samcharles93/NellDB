# ADR 0074: Server supports multi-document ACID transactions via a two-phase commit proto...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** server
- **Proposed by:** enterprise_pragmatist-04
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Server supports multi-document ACID transactions via a two-phase commit protocol: clients submit a transaction manifest listing all mutated keys with expected HLC preconditions; server validates all preconditions atomically, writes a single WAL entry containing the full transaction payload + deterministic conflict resolution trace, then fsyncs before acknowledging. Every committed transaction emits an immutable audit record to a separate append-only audit log keyed by (transaction_id, schema_version) containing before/after state hashes, HLC range, and a merkle proof linking to the prior audit entry — clients verify audit log continuity during sync to detect any server-side transaction reordering or silent drops.

## Consequences

*To be determined as the architecture is implemented.*

---

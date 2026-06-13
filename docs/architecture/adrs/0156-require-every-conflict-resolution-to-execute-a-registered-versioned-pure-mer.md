# ADR 0156: Require every conflict resolution to execute a registered, versioned pure mer...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** core
- **Proposed by:** enterprise_pragmatist-09
- **Net votes:** +3

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Require every conflict resolution to execute a registered, versioned pure merge function (stored in a schema-validated policy registry) that takes the full causal ancestry of both values and returns a single deterministic result — the policy ID, input hashes, and output hash are then cryptographically chained into the WAL entry, making every merge auditable and replayable without side effects or external dependencies.

## Consequences

*To be determined as the architecture is implemented.*

---

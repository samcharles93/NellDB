# ADR 0175: Implement a deterministic conflict resolution framework using explicit Confli...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** core
- **Proposed by:** enterprise_pragmatist-04
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Implement a deterministic conflict resolution framework using explicit Conflict Resolution Trees (CRTs): each contested key maintains a small, bounded tree of conflicting values with their HLC timestamps, origin IDs, and a deterministic merge function (e.g., semantic merge for text, vector centroid for embeddings, pixel-wise median for images) — conflicts never silently resolve via LWW; instead, unresolved branches are surfaced as first-class ConflictRecord objects in the materialized view, requiring explicit application-level resolution or policy-driven auto-merge with full audit trail (resolver ID, policy version, input hashes, output hash) written to an immutable ConflictResolutionLog for compliance replay.

## Consequences

*To be determined as the architecture is implemented.*

---

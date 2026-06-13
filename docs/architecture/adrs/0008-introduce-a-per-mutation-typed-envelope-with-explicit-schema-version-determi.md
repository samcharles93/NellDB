# ADR 0008: Introduce a per-mutation typed envelope with explicit schema version, determi...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** client
- **Proposed by:** enterprise_pragmatist-04
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Introduce a per-mutation typed envelope with explicit schema version, deterministic conflict resolution policy (LWW with causal tie-breaker via HLC), and a signed checksum; require clients to validate envelope integrity against a compiled-in schema registry before applying to local state, rejecting any mutation that fails validation or lacks a resolvable causal history.

## Consequences

*To be determined as the architecture is implemented.*

---

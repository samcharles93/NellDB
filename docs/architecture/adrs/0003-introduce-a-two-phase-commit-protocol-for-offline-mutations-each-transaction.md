# ADR 0003: Introduce a two-phase commit protocol for offline mutations: each transaction...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** client
- **Proposed by:** enterprise_pragmatist-09
- **Net votes:** +3

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Introduce a two-phase commit protocol for offline mutations: each transaction builds a local intent log with explicit preconditions (schema version, expected HLC range, DVV causal frontier) that must validate atomically at sync time before any LWW merge occurs, with rejected intents surfaced as structured conflict objects requiring explicit user resolution rather than silent overwrites.

## Consequences

*To be determined as the architecture is implemented.*

---

# ADR 0130: Implement a transactional WAL where each multi-collection write carries an ex...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** architecture
- **Proposed by:** enterprise_pragmatist-09
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Implement a transactional WAL where each multi-collection write carries an explicit conflict-resolution proof: schema-registered pure-WASM resolver modules (keyed by collection, field, and conflict-type) execute deterministically with metered gas limits, emitting a signed resolution receipt that any peer can verify without trusting peer clocks; the receipt becomes part of the causal chain, making every conflict branch auditable and replayable.

## Consequences

*To be determined as the architecture is implemented.*

---

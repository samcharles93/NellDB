# ADR 0126: Introduce a schema-enforced transaction layer where every collection declares...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** architecture
- **Proposed by:** enterprise_pragmatist-09
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Introduce a schema-enforced transaction layer where every collection declares a versioned schema (using a minimal WASM-compatible IDL) that includes pure, deterministic conflict-resolution functions per field; each multi-collection write bundles a transaction manifest listing affected keys, expected schema versions, and the resolver invocations, producing a signed resolution certificate (Ed25519 over the manifest + resolver outputs) that any peer can verify without trusting peer clocks — making every conflict branch explicit, auditable, and replayable while keeping the hot path allocation-free.

## Consequences

*To be determined as the architecture is implemented.*

---

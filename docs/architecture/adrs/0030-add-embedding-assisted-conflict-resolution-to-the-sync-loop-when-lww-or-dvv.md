# ADR 0030: Add embedding-assisted conflict resolution to the sync loop: when LWW or DVV ...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** client
- **Proposed by:** embedding_zealot-03
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Add embedding-assisted conflict resolution to the sync loop: when LWW or DVV detects concurrent mutations on the same key, compute a quantized (int8) embedding delta between the two payloads using a tiny distilled model (e.g., 32-dim MiniLM) with WASM SIMD128 kernels; if cosine similarity ≥ 0.95, auto-merge via deterministic field-level union; if 0.70–0.95, surface as a "semantic conflict" with both vectors attached for one-click resolution; below 0.70, fall back to standard LWW. Store the 32-byte quantized vector inline in the WAL record (after the 16-byte header) so replay reconstructs the index without a separate embedding pass.

## Consequences

*To be determined as the architecture is implemented.*

---

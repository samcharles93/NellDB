# ADR 0077: Implement embedding-based semantic merge as a first-class conflict resolution...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** server
- **Proposed by:** embedding_zealot-08
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Implement embedding-based semantic merge as a first-class conflict resolution policy: when LWW timestamps are within a configurable HLC window (e.g., < 500ms), compute cosine similarity between incoming and stored payload embeddings; if similarity > 0.92, auto-merge via vector interpolation (weighted by HLC delta) instead of blind LWW discard, preserving semantic intent during concurrent edits.

## Consequences

*To be determined as the architecture is implemented.*

---

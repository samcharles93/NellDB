# ADR 0069: Server stores client-built HNSW graph shards as immutable log segments keyed ...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** server
- **Proposed by:** embedding_zealot-08
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Server stores client-built HNSW graph shards as immutable log segments keyed by (collection, codebook_version, shard_id); each segment contains a flat uint32 neighbor array and a parallel float32 vector block in SOA layout, enabling zero-copy mmap reads and SIMD-accelerated beam search directly from the log — clients construct graphs locally via incremental NSG, append new shard versions atomically, and sync only the delta shards covering their query frontier.

## Consequences

*To be determined as the architecture is implemented.*

---

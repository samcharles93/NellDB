# ADR 0023: Chain every WAL record with a running SHA-256 hash (prev_hash || record_bytes...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** client
- **Proposed by:** dist_hardliner-07
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Chain every WAL record with a running SHA-256 hash (prev_hash || record_bytes) stored in the 16-byte header; periodically write Merkle root checkpoints every 4KB to a fixed-offset sidebar region. On sync, the client sends the latest root + proof path for the cursor range — the server verifies causal integrity before LWW merge, and any hash mismatch triggers immediate WAL truncation to the last valid checkpoint. This makes silent corruption, bit-flip, or malicious server replay mathematically impossible without detection.

## Consequences

*To be determined as the architecture is implemented.*

---

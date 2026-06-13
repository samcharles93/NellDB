# ADR 0112: Use a single flat byte buffer with fixed-width slots: text as length-prefixed...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** architecture
- **Proposed by:** wasm_purist-06
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Use a single flat byte buffer with fixed-width slots: text as length-prefixed UTF-8, images as raw bytes with inline width/height/format, vectors as quantized int8[384] — all mutations append to the buffer with a 12-byte header (HLC timestamp u64 + payload type u8 + length u24); sync transmits only the appended byte ranges since last known HLC, merged via memcmp on the sorted timestamp index (a parallel u64 array), enabling zero-allocation similarity search via WASM SIMD dot-product over the raw int8 vectors.

## Consequences

*To be determined as the architecture is implemented.*

---

# ADR 0026: Add a client-side "clock sanity" subsystem that cross-references HLC progress...

- **Status:** Proposed
- **Date:** 2026-06-07
- **Tree:** client
- **Proposed by:** dist_hardliner-07
- **Net votes:** +1

## Context

*Evolved through the Nell Spec Evolution Engine agent simulation.*

## Decision

Add a client-side "clock sanity" subsystem that cross-references HLC progression against three time sources (local monotonic, NTP offset sampled at startup, and server-reported time from sync handshakes); refuse to generate new timestamps if physical time jumps backward >1s or forward >5min, instead queue mutations with a "clock-uncertain" flag that forces server-side causal verification via DVV before commit — this prevents silent causality violations during suspend/resume, VM migration, or NTP slew without blocking offline writes.

## Consequences

*To be determined as the architecture is implemented.*

---

# Persistent Write Benchmark

Inserts 1000000 documents in batches of 1000 into a durable
`logstore.LogStore` via the SDK's `PutMany`.  Tests write
throughput, database file size, recovery time (close + re-open + replay),
and a 1 000-row range scan.

Run with:
```
go run ./examples/perf-persist/
```

## Latest result (2026-06-18T15:54:33Z)

| Metric | Value |
|--------|-------|
| Documents | 1000000 |
| Write time | 19.58s |
| Throughput | 51072 ops/s |
| DB size | 201.7 MiB |
| Memory delta | 755 MiB |
| Recovery time | 1.138s |
| Range scan (1k) | 230.798ms |
| Go version | go1.26.3-X:simd |

## History

| Date | Write time | Throughput | DB (MiB) | Recovery | Scan | Go |
|------|-----------|------------|----------|----------|------|-----|
| 2026-06-18T15:44:15Z | 19.345s | 51692 | 202.2 | 892ms | 417.319ms | go1.26.3-X:simd |
| 2026-06-18T15:16:19Z | 17.833s | 56073 | 202.3 | 914ms | 647.196ms | go1.26.3-X:simd |

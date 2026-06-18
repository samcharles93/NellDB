# Persistent Write Benchmark

Inserts 1000000 documents in batches of 1000 into a durable
`logstore.LogStore` via the SDK's `PutMany`.  Tests write
throughput, database file size, recovery time (close + re-open + replay),
and a 1 000-row range scan.

Run with:
```
go run ./examples/perf-persist/
```

## Latest result (2026-06-18T15:16:19Z)

| Metric | Value |
|--------|-------|
| Documents | 1000000 |
| Write time | 17.833s |
| Throughput | 56073 ops/s |
| DB size | 202.3 MiB |
| Memory delta | 823 MiB |
| Recovery time | 914ms |
| Range scan (1k) | 647.196ms |
| Go version | go1.26.3-X:simd |

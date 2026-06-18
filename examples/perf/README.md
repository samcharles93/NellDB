# In-Memory Write Benchmark

Inserts 1000000 documents in batches of 1000 into an in-memory
`nell.MemoryStore` via the SDK's `PutMany`.  Then runs a
10 000-row range scan.

Run with:
```
go run ./examples/perf/
```

## Latest result (2026-06-18T14:04:52Z)

| Metric | Value |
|--------|-------|
| Documents | 1000000 |
| Write time | 4.677s |
| Throughput | 213791 ops/s |
| Memory delta | 690 MiB |
| Range scan (10k) | 521.053ms |
| Go version | go1.26.3-X:simd |

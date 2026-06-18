# In-Memory Write Benchmark

Inserts 1000000 documents in batches of 1000 into an in-memory
`nell.MemoryStore` via the SDK's `PutMany`.  Then runs a
10 000-row range scan.

Run with:
```
go run ./examples/perf/
```

## Latest result (2026-06-18T15:54:49Z)

| Metric | Value |
|--------|-------|
| Documents | 1000000 |
| Write time | 4.087s |
| Throughput | 244677 ops/s |
| Memory delta | 611 MiB |
| Range scan (10k) | 399.859ms |
| Go version | go1.26.3-X:simd |

## History

| Date | Write time | Throughput (ops/s) | Mem (MiB) | Scan (10k) | Go |
|------|-----------|--------------------|------------|------------|-----|
| 2026-06-18T15:48:11Z | 3.985s | 250896 | 575 | 396.091ms | go1.26.3-X:simd |
| 2026-06-18T14:04:52Z | 4.677s | 213791 | 690 | 521.053ms | go1.26.3-X:simd |

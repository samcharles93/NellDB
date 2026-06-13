package main

import (
	"context"
	"fmt"
	"log"
	"runtime"
	"time"

	"github.com/samcharles93/NellDB"
	"github.com/samcharles93/NellDB/sdk"
)

func main() {
	const count = 1_000_000
	const batchSize = 1000

	ctx := context.Background()
	store := nell.NewMemoryStore("benchmark-node")
	db := sdk.New(store, "benchmark-node", nell.DefaultCollection)

	fmt.Printf("▸ Starting benchmark: 1,000,000 rows in batches of %d\n", batchSize)

	start := time.Now()
	var memBefore runtime.MemStats
	runtime.ReadMemStats(&memBefore)

	for i := 0; i < count; i += batchSize {
		batch := make([]sdk.Doc, batchSize)
		for j := range batchSize {
			id := i + j
			batch[j] = sdk.Doc{
				sdk.FieldID: fmt.Sprintf("doc:%07d", id),
				"payload":   "some-random-data-to-fill-space-a-bit",
				"index":     id,
				"timestamp": time.Now().UnixNano(),
			}
		}

		_, err := db.PutMany(ctx, batch)
		if err != nil {
			log.Fatalf("PutMany failed at i=%d: %v", i, err)
		}

		if (i+batchSize)%100_000 == 0 {
			elapsed := time.Since(start)
			ops := float64(i+batchSize) / elapsed.Seconds()
			fmt.Printf("  Progress: %d/%d (%.1f ops/s)\n", i+batchSize, count, ops)
		}
	}

	totalTime := time.Since(start)
	var memAfter runtime.MemStats
	runtime.ReadMemStats(&memAfter)

	fmt.Printf("\n▸ Benchmark Complete\n")
	fmt.Printf("  Total Time:   %v\n", totalTime)
	fmt.Printf("  Avg Throughput: %.1f ops/s\n", float64(count)/totalTime.Seconds())
	fmt.Printf("  Memory Delta: %d MiB\n", (memAfter.Alloc-memBefore.Alloc)/1024/1024)
	fmt.Printf("  Final Info:   %+v\n\n", db.Info())

	// Range Scan Test
	fmt.Println("▸ Testing Range Scan (10,000 rows)...")
	scanStart := time.Now()
	rows, err := db.AllDocs(ctx, sdk.DocRange{
		StartKey: "doc:0050000",
		EndKey:   "doc:0060000",
	})
	if err != nil {
		log.Fatalf("Range scan failed: %v", err)
	}
	fmt.Printf("  Scan Time: %v (found %d rows)\n", time.Now().Sub(scanStart), len(rows.Rows))
}

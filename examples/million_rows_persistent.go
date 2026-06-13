package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"runtime"
	"time"

	"github.com/samcharles93/NellDB/logstore"
	"github.com/samcharles93/NellDB/sdk"
)

func main() {
	const count = 1_000_000 
	const batchSize = 1000
	const dbPath = "bench_persistent.db"

	// Cleanup previous run
	os.Remove(dbPath)
	defer os.Remove(dbPath)

	ctx := context.Background()
	store, err := logstore.OpenLog(dbPath, "benchmark-node-persistent")
	if err != nil {
		log.Fatalf("failed to open logstore: %v", err)
	}
	defer store.Close()

	db := sdk.New(store, "benchmark-node-persistent", "benchmark")

	fmt.Printf("▸ Starting Persistent benchmark: %d rows in batches of %d\n", count, batchSize)
	fmt.Printf("  Database path: %s\n", dbPath)
	
	start := time.Now()
	var memBefore runtime.MemStats
	runtime.ReadMemStats(&memBefore)

	for i := 0; i < count; i += batchSize {
		batch := make([]sdk.Doc, batchSize)
		for j := 0; j < batchSize; j++ {
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
			elapsed := time.Now().Sub(start)
			ops := float64(i+batchSize) / elapsed.Seconds()
			fmt.Printf("  Progress: %d/%d (%.1f ops/s)\n", i+batchSize, count, ops)
		}
	}

	totalTime := time.Now().Sub(start)
	var memAfter runtime.MemStats
	runtime.ReadMemStats(&memAfter)

	fileInfo, _ := os.Stat(dbPath)

	fmt.Printf("\n▸ Write Benchmark Complete\n")
	fmt.Printf("  Total Time:   %v\n", totalTime)
	fmt.Printf("  Avg Throughput: %.1f ops/s\n", float64(count)/totalTime.Seconds())
	fmt.Printf("  Database Size: %.2f MiB\n", float64(fileInfo.Size())/1024/1024)
	fmt.Printf("  Memory Delta: %d MiB\n", (memAfter.Alloc-memBefore.Alloc)/1024/1024)

	// Re-open performance test (Recovery)
	fmt.Println("\n▸ Testing Recovery Performance (closing and re-opening)...")
	store.Close()
	
	recoverStart := time.Now()
	store2, err := logstore.OpenLog(dbPath, "benchmark-node-persistent")
	if err != nil {
		log.Fatalf("failed to re-open logstore: %v", err)
	}
	defer store2.Close()
	recoverTime := time.Now().Sub(recoverStart)
	
	fmt.Printf("  Recovery Time: %v (%.1f docs/s)\n", recoverTime, float64(count)/recoverTime.Seconds())

	// Range Scan Test
	db2 := sdk.New(store2, "benchmark-node-persistent", "benchmark")
	fmt.Println("\n▸ Testing Range Scan (1,000 rows)...")
	scanStart := time.Now()
	rows, err := db2.AllDocs(ctx, sdk.DocRange{
		StartKey: "doc:0050000",
		EndKey:   "doc:0051000",
	})
	if err != nil {
		log.Fatalf("Range scan failed: %v", err)
	}
	fmt.Printf("  Scan Time: %v (found %d rows)\n", time.Now().Sub(scanStart), len(rows.Rows))
}

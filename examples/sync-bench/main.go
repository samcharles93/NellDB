package main

import (
	"context"
	"fmt"
	"log"
	"net/http/httptest"
	"os"
	"time"

	"github.com/samcharles93/NellDB"
	"github.com/samcharles93/NellDB/logstore"
	"github.com/samcharles93/NellDB/sdk"
	"github.com/samcharles93/NellDB/server"
)

func main() {
	const count = 100_000 // 100k rows for binary sync benchmark
	const dbPathServer = "bench_sync_server.db"
	const dbPathClient = "bench_sync_client.db"

	// Cleanup
	os.Remove(dbPathServer)
	os.Remove(dbPathClient)
	defer os.Remove(dbPathServer)
	defer os.Remove(dbPathClient)

	ctx := context.Background()

	// ── 1. Setup Server ───────────────────────────────────────────────────
	fmt.Printf("▸ Setting up server with %d records...\n", count)
	serverStore, err := logstore.OpenLog(dbPathServer, "server-node")
	if err != nil {
		log.Fatal(err)
	}

	// Fast populate server directly through store to bypass SDK overhead
	clock := nell.NewHLC()
	for i := range count {
		id := fmt.Sprintf("doc:%07d", i)
		// Fake a strictly increasing clock by manually bumping WallTime
		// every few records to ensure we don't just rely on the counter
		// which might be sensitive to map iteration order.
		rec := nell.Record{
			Collection: "benchmark",
			ID:         id,
			Type:       nell.TypeText,
			Payload:    fmt.Appendf(nil, `{"_id":%q,"val":%d,"payload":"some-data"}`, id, i),
			UpdatedBy:  "server-node",
			Clock:      nell.HLC{WallTime: clock.WallTime + int64(i), Counter: 0},
		}
		_, _, err := serverStore.Put(rec)
		if err != nil {
			log.Fatalf("Server populate failed at %d: %v", i, err)
		}
	}

	allOnServer, _ := serverStore.List("benchmark")
	fmt.Printf("  Server store has %d records\n", len(allOnServer))

	srv := server.New(serverStore, "server-node")
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	fmt.Printf("  Server ready at %s\n", ts.URL)

	// ── 2. Setup Client ───────────────────────────────────────────────────
	clientStore, err := logstore.OpenLog(dbPathClient, "client-node")
	if err != nil {
		log.Fatal(err)
	}
	clientDB := sdk.New(clientStore, "client-node", "benchmark")
	replicator := sdk.NewReplicator(clientDB, ts.URL)

	// ── 3. Run Sync Benchmark ─────────────────────────────────────────────
	fmt.Printf("\n▸ Starting Binary Sync Benchmark (Pulling %d rows)...\n", count)

	start := time.Now()

	// Pull until we have everything
	totalIngested := 0
	for {
		ingested, err := replicator.Pull(ctx)
		if err != nil {
			log.Fatalf("Pull failed: %v", err)
		}
		totalIngested += ingested
		if ingested == 0 {
			break
		}
		// fmt.Printf("  Progress: %d/%d rows ingested\n", totalIngested, count)
	}

	elapsed := time.Since(start)

	fmt.Printf("\n▸ Binary Sync Complete\n")
	fmt.Printf("  Total Time:     %v\n", elapsed)
	fmt.Printf("  Throughput:     %.1f docs/s\n", float64(totalIngested)/elapsed.Seconds())

	serverFileInfo, _ := os.Stat(dbPathServer)
	clientFileInfo, _ := os.Stat(dbPathClient)
	fmt.Printf("  Server DB Size: %.2f MiB\n", float64(serverFileInfo.Size())/1024/1024)
	fmt.Printf("  Client DB Size: %.2f MiB\n", float64(clientFileInfo.Size())/1024/1024)
}

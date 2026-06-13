// Command perf-persist runs a durable-store write benchmark (1M rows) and
// logs results to results.json for display in README.md.
//
// Usage:
//
//	go run ./examples/perf-persist/
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"runtime"
	"strings"
	"text/template"
	"time"

	"github.com/samcharles93/NellDB/logstore"
	"github.com/samcharles93/NellDB/sdk"
)

const (
	count          = 1_000_000
	batchSize      = 1000
	dbPath         = "bench_persistent.db"
	resultsRelPath = "examples/perf-persist/results.json"
)

// Result holds one benchmark run for the persistent store.
type Result struct {
	Date         string  `json:"date"`
	GoVersion    string  `json:"go_version"`
	Count        int     `json:"count"`
	BatchSize    int     `json:"batch_size"`
	WriteTime    string  `json:"write_time"`
	Throughput   float64 `json:"throughput_ops_per_sec"`
	DBSizeMB     float64 `json:"db_size_mb"`
	MemDeltaMB   int64   `json:"mem_delta_mb"`
	RecoveryTime string  `json:"recovery_time"`
	ScanTime     string  `json:"scan_time_1k"`
}

func main() {
	os.Remove(dbPath)
	defer os.Remove(dbPath)

	ctx := context.Background()
	store, err := logstore.OpenLog(dbPath, "benchmark-node-persistent")
	if err != nil {
		log.Fatalf("failed to open logstore: %v", err)
	}

	db := sdk.New(store, "benchmark-node-persistent", "benchmark")

	fmt.Printf("▸ Starting Persistent benchmark: %d rows in batches of %d\n", count, batchSize)
	fmt.Printf("  Database path: %s\n", dbPath)

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

		if _, err := db.PutMany(ctx, batch); err != nil {
			log.Fatalf("PutMany failed at i=%d: %v", i, err)
		}

		if (i+batchSize)%100_000 == 0 {
			elapsed := time.Since(start)
			ops := float64(i+batchSize) / elapsed.Seconds()
			fmt.Printf("  Progress: %d/%d (%.1f ops/s)\n", i+batchSize, count, ops)
		}
	}

	writeTime := time.Since(start)
	var memAfter runtime.MemStats
	runtime.ReadMemStats(&memAfter)

	fileInfo, _ := os.Stat(dbPath)

	fmt.Printf("\n▸ Write Benchmark Complete\n")
	fmt.Printf("  Total Time:   %v\n", writeTime)
	fmt.Printf("  Avg Throughput: %.1f ops/s\n", float64(count)/writeTime.Seconds())
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
	recoveryTime := time.Since(recoverStart)

	fmt.Printf("  Recovery Time: %v (%.1f docs/s)\n", recoveryTime, float64(count)/recoveryTime.Seconds())

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
	scanTime := time.Since(scanStart)
	fmt.Printf("  Scan Time: %v (found %d rows)\n", scanTime, len(rows.Rows))

	// ── Persist results ──────────────────────────────────────────────
	r := Result{
		Date:         time.Now().UTC().Format(time.RFC3339),
		GoVersion:    runtime.Version(),
		Count:        count,
		BatchSize:    batchSize,
		WriteTime:    writeTime.Truncate(time.Millisecond).String(),
		Throughput:   float64(count) / writeTime.Seconds(),
		DBSizeMB:     float64(fileInfo.Size()) / (1024 * 1024),
		MemDeltaMB:   int64(memAfter.Alloc-memBefore.Alloc) / (1024 * 1024),
		RecoveryTime: recoveryTime.Truncate(time.Millisecond).String(),
		ScanTime:     scanTime.Truncate(time.Microsecond).String(),
	}

	results, err := loadResults()
	if err != nil {
		log.Printf("loading previous results: %v (starting fresh)", err)
	}
	results = append(results, r)
	if err := saveResults(results); err != nil {
		log.Fatalf("saving results: %v", err)
	}

	fmt.Printf("\nResults saved to results.json (%d runs total)\n", len(results))
}

// ── Result persistence ──────────────────────────────────────────────

func loadResults() ([]Result, error) {
	data, err := os.ReadFile(resultsRelPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var results []Result
	if err := json.Unmarshal(data, &results); err != nil {
		return nil, err
	}
	return results, nil
}

func saveResults(results []Result) error {
	data, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(resultsRelPath, data, 0o644); err != nil {
		return err
	}
	return generateREADME(results)
}

var readmeTmpl = template.Must(template.New("README").Parse(`# Persistent Write Benchmark

Inserts {{.Count}} documents in batches of {{.BatchSize}} into a durable
` + "`" + `logstore.LogStore` + "`" + ` via the SDK's ` + "`" + `PutMany` + "`" + `.  Tests write
throughput, database file size, recovery time (close + re-open + replay),
and a 1 000-row range scan.

Run with:
` + "```" + `
go run ./examples/perf-persist/
` + "```" + `

## Latest result ({{.Latest.Date}})

| Metric | Value |
|--------|-------|
| Documents | {{.Latest.Count}} |
| Write time | {{.Latest.WriteTime}} |
| Throughput | {{printf "%.0f" .Latest.Throughput}} ops/s |
| DB size | {{printf "%.1f" .Latest.DBSizeMB}} MiB |
| Memory delta | {{.Latest.MemDeltaMB}} MiB |
| Recovery time | {{.Latest.RecoveryTime}} |
| Range scan (1k) | {{.Latest.ScanTime}} |
| Go version | {{.Latest.GoVersion}} |

{{- if gt (len .History) 1 }}

## History

| Date | Write time | Throughput | DB (MiB) | Recovery | Scan | Go |
|------|-----------|------------|----------|----------|------|-----|
{{range .History -}}
| {{.Date}} | {{.WriteTime}} | {{printf "%.0f" .Throughput}} | {{printf "%.1f" .DBSizeMB}} | {{.RecoveryTime}} | {{.ScanTime}} | {{.GoVersion}} |
{{end}}
{{- end }}
`))

func generateREADME(results []Result) error {
	if len(results) == 0 {
		return nil
	}
	data := struct {
		Count     int
		BatchSize int
		Latest    Result
		History   []Result
	}{
		Count:     count,
		BatchSize: batchSize,
		Latest:    results[len(results)-1],
	}
	if len(results) > 1 {
		data.History = make([]Result, len(results)-1)
		for i := len(results) - 2; i >= 0; i-- {
			data.History[len(results)-2-i] = results[i]
		}
	}

	var buf bytes.Buffer
	if err := readmeTmpl.Execute(&buf, data); err != nil {
		return err
	}
	out := strings.TrimSpace(buf.String()) + "\n"
	return os.WriteFile("examples/perf-persist/README.md", []byte(out), 0o644)
}

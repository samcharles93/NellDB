// Command example is a runnable tour of the Nell SDK.  It exercises every
// public method on DocDB against an in-process nell-server so you can read
// the code top-to-bottom and see how the pieces fit together.
//
// Usage:
//
//	# In-memory only (no server):
//	go run ./examples/
//
//	# Against a real nell-server (must be running on :9342):
//	go run ./examples/ --server http://localhost:9342
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/samcharles93/NellDB"
	"github.com/samcharles93/NellDB/sdk"
)

func main() {
	serverURL := flag.String("server", "", "base URL of a nell-server to replicate against (default: in-memory only)")
	flag.Parse()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// SIGINT/Ctrl-C to stop cleanly
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sig
		fmt.Println("\nshutting down…")
		cancel()
	}()

	// ── 1. Open a local store and wrap it as a DocDB ─────────────────────
	// MemoryStore is fine for examples; swap in logstore.OpenLog for persistence.
	store := nell.NewMemoryStore("example-node")
	db := sdk.New(store, "example-node")

	fmt.Println("▸ opened db")
	fmt.Printf("  %+v\n\n", db.Info())

	// ── 2. Put a few docs ─────────────────────────────────────────────────
	putRev, err := db.Put(ctx, sdk.Doc{
		sdk.FieldID: "note:1",
		"title":     "First note",
		"body":      "Hello, world.",
		"tags":      []any{"intro", "demo"},
	})
	must(err)
	fmt.Printf("▸ put note:1 rev=%s\n", putRev)

	// Bulk: PutMany is atomic — any conflict rolls back the rest.
	revs, err := db.PutMany(ctx, []sdk.Doc{
		{sdk.FieldID: "note:2", "title": "Second"},
		{sdk.FieldID: "note:3", "title": "Third"},
		{sdk.FieldID: "note:4", "title": "Fourth"},
	})
	must(err)
	fmt.Printf("▸ putMany 3 docs, revs=%v\n\n", revs)

	// ── 3. Read back ──────────────────────────────────────────────────────
	got, err := db.Get(ctx, "note:1")
	must(err)
	fmt.Printf("▸ get note:1: %v\n", got)

	// GetMany — missing ids are silently skipped
	many, err := db.GetMany(ctx, []string{"note:1", "note:2", "note:ghost"})
	must(err)
	fmt.Printf("▸ getMany got %d of 3 ids\n\n", len(many))

	// ── 4. Read-modify-write with conflict detection ─────────────────────
	got, err = db.Get(ctx, "note:1")
	must(err)
	got["title"] = "First note (edited)"
	newRev, err := db.Put(ctx, got)
	must(err)
	fmt.Printf("▸ updated note:1, new rev=%s\n", newRev)

	// Stale rev → ErrConflict
	_, err = db.Put(ctx, sdk.Doc{
		sdk.FieldID:  "note:1",
		sdk.FieldRev: putRev, // the original rev, now stale
		"title":      "Should fail",
	})
	if !errors.Is(err, sdk.ErrConflict) {
		log.Fatalf("expected ErrConflict, got %v", err)
	}
	fmt.Println("▸ stale rev correctly produced ErrConflict")

	// ── 5. AllDocs range scan (common pattern: scan a key prefix) ─────
	rows, err := db.AllDocs(ctx, sdk.DocRange{
		StartKey:     "note:",
		EndKey:       "note:\ufff0",
		InclusiveEnd: true,
		IncludeDocs:  true,
	})
	must(err)
	fmt.Printf("▸ allDocs prefix 'note:' returned %d rows\n", len(rows.Rows))
	for _, r := range rows.Rows {
		fmt.Printf("  %s  rev=%s\n", r.ID, r.Value.Rev)
	}
	fmt.Println()

	// ── 6. Changes feed ───────────────────────────────────────────────────
	// Subscribe before doing more writes so we see them.
	changes := db.Changes(ctx)
	go func() {
		for c := range changes {
			fmt.Printf("  [change] %s rev=%s deleted=%v\n", c.ID, c.Rev, c.Deleted)
		}
	}()
	// Give the subscription a moment to wire up.
	time.Sleep(50 * time.Millisecond)

	_, _ = db.Put(ctx, sdk.Doc{sdk.FieldID: "note:5", "title": "Live update"})
	_, _ = db.Remove(ctx, "note:5")
	time.Sleep(100 * time.Millisecond)

	// ── 7. Replicate (only if --server was given) ────────────────────────
	if *serverURL != "" {
		fmt.Printf("\n▸ replicating to %s\n", *serverURL)
		rep := sdk.NewReplicator(db, *serverURL)

		pushed, pulled, err := rep.Sync(ctx)
		must(err)
		fmt.Printf("  pushed=%d pulled=%d\n", pushed, pulled)

		// Open a second client and pull to prove the data made the round-trip.
		fmt.Println("\n▸ opening a second client to verify the round-trip")
		store2 := nell.NewMemoryStore("example-client-2")
		db2 := sdk.New(store2, "example-client-2")
		rep2 := sdk.NewReplicator(db2, *serverURL)
		pulled, err = rep2.Pull(ctx)
		must(err)
		fmt.Printf("  pulled=%d\n", pulled)
		fmt.Printf("  client-2 info: %+v\n", db2.Info())
	} else {
		fmt.Println("\n▸ skipping replication (pass --server to enable)")
	}

	// ── 8. Final snapshot ─────────────────────────────────────────────────
	fmt.Println("\n▸ final state:")
	fmt.Printf("  %+v\n", db.Info())
}

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

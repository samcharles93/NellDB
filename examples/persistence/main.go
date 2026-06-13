package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/samcharles93/NellDB/logstore"
	"github.com/samcharles93/NellDB/sdk"
)

func main() {
	dbPath := "example_persistent.db"
	nodeID := "persistent-node"
	ctx := context.Background()

	// Clean up previous runs
	os.Remove(dbPath)
	defer os.Remove(dbPath)

	fmt.Printf("▸ Initializing persistent LogStore at %s\n", dbPath)

	// 1. Open a persistent LogStore
	// This uses an append-only log with Zstd compression and a binary format.
	store, err := logstore.OpenLog(dbPath, nodeID)
	if err != nil {
		log.Fatalf("Failed to open LogStore: %v", err)
	}

	// 2. Wrap it in a DocDB SDK
	db := sdk.New(store, nodeID, "persistent_collection")

	// 3. Write some data
	_, err = db.Put(ctx, sdk.Doc{
		sdk.FieldID: "doc:1",
		"message":   "This data is durable",
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("▸ Data written and flushed to disk")

	// 4. Close the store (simulating an application restart)
	store.Close()
	fmt.Println("▸ Store closed (simulated restart)")

	// 5. Re-open the store
	// The store will perform a high-performance parallel binary replay.
	fmt.Println("▸ Re-opening store and replaying log...")
	reopenedStore, err := logstore.OpenLog(dbPath, nodeID)
	if err != nil {
		log.Fatalf("Failed to re-open: %v", err)
	}
	defer reopenedStore.Close()

	// 6. Verify data survived the restart
	reopenedDB := sdk.New(reopenedStore, nodeID, "persistent_collection")
	doc, err := reopenedDB.Get(ctx, "doc:1")
	if err != nil {
		log.Fatalf("Failed to retrieve data after restart: %v", err)
	}

	fmt.Printf("▸ Successfully recovered document: %v\n", doc)
	
	info := reopenedDB.Info()
	fmt.Printf("▸ Final DB Info: %+v\n", info)
}

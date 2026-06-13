package main

import (
	"context"
	"fmt"
	"log"

	"github.com/samcharles93/NellDB"
	"github.com/samcharles93/NellDB/sdk"
)

func main() {
	ctx := context.Background()

	// 1. Initialize an in-memory store
	// The node ID "local-client" is used for deterministic conflict resolution
	store := nell.NewMemoryStore("local-client")

	// 2. Wrap it in a DocDB SDK
	// This provides the high-level document API (_id, _rev, etc.)
	db := sdk.New(store, "local-client", "notes")

	fmt.Println("▸ Created in-memory database")

	// 3. Put a document
	docID := "note:hello"
	rev, err := db.Put(ctx, sdk.Doc{
		sdk.FieldID: docID,
		"title":     "Hello NellDB",
		"content":   "This is a basic example of CRUD operations.",
		"tags":      []string{"example", "basic"},
	})
	if err != nil {
		log.Fatalf("Put failed: %v", err)
	}
	fmt.Printf("▸ Put document %q (rev: %s)\n", docID, rev)

	// 4. Get the document back
	doc, err := db.Get(ctx, docID)
	if err != nil {
		log.Fatalf("Get failed: %v", err)
	}
	fmt.Printf("▸ Retrieved document: %v\n", doc)

	// 5. Update the document
	// Note: We include the _rev to ensure we're updating the version we just read
	doc["title"] = "Updated Title"
	newRev, err := db.Put(ctx, doc)
	if err != nil {
		log.Fatalf("Update failed: %v", err)
	}
	fmt.Printf("▸ Updated document %q (new rev: %s)\n", docID, newRev)

	// 6. List all documents in the collection
	result, err := db.AllDocs(ctx, sdk.DocRange{IncludeDocs: true})
	if err != nil {
		log.Fatalf("AllDocs failed: %v", err)
	}
	fmt.Printf("▸ Found %d documents in collection 'notes':\n", result.TotalRows)
	for _, row := range result.Rows {
		fmt.Printf("  - %s: %v\n", row.ID, row.Doc["title"])
	}

	// 7. Delete the document
	_, err = db.Remove(ctx, docID)
	if err != nil {
		log.Fatalf("Remove failed: %v", err)
	}
	fmt.Printf("▸ Removed document %q\n", docID)

	// Verify deletion
	_, err = db.Get(ctx, docID)
	if err == sdk.ErrNotFound {
		fmt.Println("▸ Confirmed: document is no longer available")
	}
}

package main

import (
	"context"
	"fmt"
	"log"
	"net/http/httptest"

	"github.com/samcharles93/NellDB"
	"github.com/samcharles93/NellDB/sdk"
	"github.com/samcharles93/NellDB/server"
)

func main() {
	ctx := context.Background()

	// 1. Setup the Server
	// In a real app, this would be a standalone process (nelldb-server)
	serverStore := nell.NewMemoryStore("hub-server")
	srv := server.New(serverStore, "hub-server")
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	fmt.Printf("▸ Server started at %s\n", ts.URL)

	// 2. Setup Client A
	storeA := nell.NewMemoryStore("client-a")
	dbA := sdk.New(storeA, "client-a", "sync_demo")
	repA := sdk.NewReplicator(dbA, ts.URL)

	// 3. Setup Client B
	storeB := nell.NewMemoryStore("client-b")
	dbB := sdk.New(storeB, "client-b", "sync_demo")
	repB := sdk.NewReplicator(dbB, ts.URL)

	// 4. Client A writes a document
	fmt.Println("▸ Client A: Writing document...")
	_, err := dbA.Put(ctx, sdk.Doc{
		sdk.FieldID: "shared:doc",
		"owner":     "client-a",
		"text":      "Sync is powerful",
	})
	if err != nil {
		log.Fatal(err)
	}

	// 5. Client A Pushes to the server
	fmt.Println("▸ Client A: Pushing changes to server (Binary Protocol)...")
	pushed, err := repA.Push(ctx)
	if err != nil {
		log.Fatalf("Push A failed: %v", err)
	}
	fmt.Printf("  Pushed %d documents\n", pushed)

	// 6. Client B Pulls from the server
	fmt.Println("▸ Client B: Pulling changes from server (Binary Protocol)...")
	pulled, err := repB.Pull(ctx)
	if err != nil {
		log.Fatalf("Pull B failed: %v", err)
	}
	fmt.Printf("  Pulled %d documents\n", pulled)

	// 7. Verify Client B has the document
	doc, err := dbB.Get(ctx, "shared:doc")
	if err != nil {
		log.Fatalf("Client B failed to find synced document: %v", err)
	}
	fmt.Printf("▸ Client B: Successfully received synced document: %v\n", doc)

	// 8. Demonstrate Conflict Resolution (LWW)
	fmt.Println("\n▸ Demonstrating Conflict Resolution (Client B updates document)...")
	doc["text"] = "Updated by Client B"
	dbB.Put(ctx, doc)
	repB.Push(ctx)

	fmt.Println("▸ Client A: Pulling update from Client B...")
	repA.Pull(ctx)
	docA, _ := dbA.Get(ctx, "shared:doc")
	fmt.Printf("  Client A now sees: %v\n", docA["text"])
}

// Package sdk is the application-facing layer of NellDB — a distributed,
// real-time, document-oriented database with HTTP-based sync.
//
// NellDB stores arbitrary JSON documents keyed by _id, with MVCC
// revisions for safe concurrent writes, a live changes feed for real-time
// updates, and HTTP replication between any number of nodes.  No separate
// daemon: the server is a Go library you import, the client is a JS class
// that talks to it over HTTP.
//
// # Mental model
//
// A Doc is an arbitrary JSON object identified by _id and versioned by
// _rev (an MVCC "<gen>-<sha1>" token).  Tombstones use _deleted=true.
// The SDK translates between user-facing Docs and the wire-level
// nell.Record that the engine and the server speak.
//
// Reserved fields:
//
//	_id       — document key (required)
//	_rev      — MVCC token, set by the SDK on Put
//	_deleted  — tombstone marker, set by Remove
//
// Everything else in the Doc map is application data and round-trips
// unchanged through Put / Get / replication.
//
// # Quick start
//
//	db := sdk.New(nell.NewMemoryStore("client"), "client")
//	ctx := context.Background()
//
//	rev, _ := db.Put(ctx, sdk.Doc{
//	    sdk.FieldID: "note:1",
//	    "title":    "Hello",
//	    "body":     "world",
//	})
//
//	doc, _ := db.Get(ctx, "note:1")
//	doc["title"] = "Updated"
//	_, err := db.Put(ctx, doc)
//	if errors.Is(err, sdk.ErrConflict) {
//	    // another writer changed note:1 between our Get and Put
//	}
//
// # Replication
//
// Nell nodes sync over HTTP — no broker, no separate process.  The
// Replicator speaks the standard /sync/pull, /sync/push, and /sync/check
// endpoints exposed by any node that imports the server package.  Pull
// uses /sync/check (per-peer KnowledgeVector) so concurrent writes from
// a new peer are not silently dropped.
//
//	rep := sdk.NewReplicator(db, "https://home.example.com")
//	pushed, pulled, _ := rep.Sync(ctx)
//
//	// Or run continuously with backoff on errors:
//	stop := rep.Live(ctx, sdk.LiveConfig{Interval: 5 * time.Second})
//	defer stop()
//
// # Internal records
//
// The SDK persists two records into the same nell.Store that hold
// replication state:
//
//	meta:clock   — highest HLC ever observed (legacy)
//	meta:vector  — per-peer KnowledgeVector (drives /sync/check)
//
// Both are filtered from outgoing Push and incoming ingestRemote so they
// never leak to peers.  AllDocs queries that don't use a range filter
// will see them; filter by prefix or restrict your range.
//
// # Limitations
//
// v0.1 has no compaction, no WebSocket sync, no Mango-style queries, and
// no attachments.  Concurrent writes are resolved at the engine by LWW
// on HLC; the SDK's _rev detects stale local writes but cross-node
// conflicts still resolve at the engine.
package sdk

import "errors"

// Reserved field names inside a Doc.  Apps must not treat these as user data.
const (
	// FieldID is the document key.  Required on every Put.
	FieldID = "_id"
	// FieldRev is the MVCC "<gen>-<sha1>" revision token.  Set by the SDK
	// on Put; apps read it to detect stale writes.
	FieldRev = "_rev"
	// FieldDeleted is the tombstone marker.  Set by Remove.
	FieldDeleted = "_deleted"
	// FieldType discriminates the kind of payload in the underlying Record.
	FieldType = "_type"
	// FieldVector holds the float32 array for vector similarity search.
	FieldVector = "_vector"
)

// Doc is the user-facing document.  A document is a flat JSON object — _id,
// _rev, and _deleted are reserved for the SDK; every other key is application
// data that round-trips unchanged through Put / Get / replication.
//
//	doc := sdk.Doc{
//	    sdk.FieldID: "note:1",
//	    "title":     "Hello",
//	    "tags":      []any{"a", "b"},
//	}
type Doc map[string]any

// Change is a single entry in a changes feed.  Doc is populated when the
// emitter has the full document in hand (always for local changes, sometimes
// for remote changes).
type Change struct {
	ID      string `json:"id"`
	Rev     string `json:"rev"`
	Deleted bool   `json:"deleted"`
	Doc     Doc    `json:"doc,omitempty"`
}

// DocRange describes the parameters to AllDocs.  The zero value returns every
// non-deleted document.
type DocRange struct {
	StartKey     string   // inclusive lower bound (Couch-style: "m:" starts at "m:...")
	EndKey       string   // inclusive upper bound unless InclusiveEnd is false
	InclusiveEnd bool     // when false, EndKey is exclusive
	Limit        int      // 0 = unlimited
	Skip         int      // offset
	Keys         []string // if set, fetches only these IDs (in order)
	IncludeDocs  bool     // attach the full Doc to each row
}

// DocRow is a single row in an AllDocs result.
type DocRow struct {
	ID    string `json:"id"`
	Key   string `json:"key"`
	Value struct {
		Rev string `json:"rev"`
	} `json:"value"`
	Doc Doc `json:"doc,omitempty"`
}

// AllDocsResult is the response shape of AllDocs.
type AllDocsResult struct {
	TotalRows int      `json:"total_rows"`
	Offset    int      `json:"offset"`
	Rows      []DocRow `json:"rows"`
}

// Sentinel errors.  Match with errors.Is.
var (
	// ErrNotFound is returned by Get when the id is missing or tombstoned.
	ErrNotFound = errors.New("sdk: document not found")
	// ErrConflict is returned by Put and Remove when the supplied _rev does
	// not match the current local rev.
	ErrConflict = errors.New("sdk: revision conflict")
	// ErrMissingID is returned by Put and Remove when the doc has no _id.
	ErrMissingID = errors.New("sdk: document is missing _id")
)

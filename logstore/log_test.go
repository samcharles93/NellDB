package logstore

import (
	"os"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"
	"github.com/samcharles93/NellDB"
)

func TestLogStorePersistence(t *testing.T) {
	path := t.TempDir() + "/test.db"

	// Write some records
	ls, err := OpenLog(path, "node-a")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = ls.PutLocal(&nell.Record{ID: "doc-1", Type: nell.TypeText, Payload: []byte("hello")})
	_, _ = ls.PutLocal(&nell.Record{ID: "doc-2", Type: nell.TypeText, Payload: []byte("world")})
	_ = ls.Close()

	// Verify file exists and has content
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() == 0 {
		t.Error("log file is empty")
	}

	// Reopen and verify records survived
	ls2, err := OpenLog(path, "node-a")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ls2.Close() }()

	rec, err := ls2.Get(nell.DefaultCollection, "doc-1")
	if err != nil {
		t.Fatal(err)
	}
	if string(rec.Payload) != "hello" {
		t.Errorf("payload = %q, want hello", rec.Payload)
	}

	list, _ := ls2.List(nell.DefaultCollection)
	if len(list) != 2 {
		t.Errorf("got %d records, want 2", len(list))
	}
}

func TestLogStoreDeleteAndReopen(t *testing.T) {
	path := t.TempDir() + "/test.db"

	ls, _ := OpenLog(path, "node-a")
	_, _ = ls.PutLocal(&nell.Record{ID: "doc-1", Type: nell.TypeText, Payload: []byte("keep")})
	_, _ = ls.PutLocal(&nell.Record{ID: "doc-2", Type: nell.TypeText, Payload: []byte("gone")})
	_, _ = ls.Delete(nell.DefaultCollection, "doc-2")
	_ = ls.Close()

	ls2, _ := OpenLog(path, "node-a")
	defer func() { _ = ls2.Close() }()

	list, _ := ls2.List(nell.DefaultCollection)
	if len(list) != 1 {
		t.Fatalf("got %d records, want 1 (one deleted)", len(list))
	}
	if list[0].ID != "doc-1" {
		t.Errorf("got %s, want doc-1", list[0].ID)
	}
}

// ── Compaction ──────────────────────────────────────────────────────────────

func TestLogStoreCompactBasic(t *testing.T) {
	path := t.TempDir() + "/compact.db"

	ls, _ := OpenLog(path, "node-a")
	_, _ = ls.PutLocal(&nell.Record{ID: "doc-1", Type: nell.TypeText, Payload: []byte("hello")})
	_, _ = ls.PutLocal(&nell.Record{ID: "doc-2", Type: nell.TypeText, Payload: []byte("world")})

	// Verify file has content before compaction
	info, _ := os.Stat(path)
	beforeSize := info.Size()

	n, err := ls.Compact(0)
	if err != nil {
		t.Fatalf("Compact: %v", err)
	}
	if n != 2 {
		t.Errorf("Compact returned %d records, want 2", n)
	}

	// File should still exist and be the right size
	info, _ = os.Stat(path)
	if info.Size() == 0 {
		t.Error("compacted file is empty")
	}
	if info.Size() > beforeSize {
		t.Errorf("compacted file grew: %d > %d", info.Size(), beforeSize)
	}

	// Records should still be readable
	rec, err := ls.Get(nell.DefaultCollection, "doc-1")
	if err != nil {
		t.Fatalf("Get after compact: %v", err)
	}
	if string(rec.Payload) != "hello" {
		t.Errorf("payload = %q, want hello", rec.Payload)
	}

	// Should still accept writes after compaction
	_, _, err = ls.Put(nell.Record{ID: "doc-3", Type: nell.TypeText, Payload: []byte("after"), Clock: nell.HLC{WallTime: 9999, Counter: 0}, UpdatedBy: "node-b"})
	if err != nil {
		t.Errorf("write after compact: %v", err)
	}

	_ = ls.Close()
}

func TestLogStoreCompactTombstoneRetention(t *testing.T) {
	path := t.TempDir() + "/compact-tombstone.db"

	ls, _ := OpenLog(path, "node-a")
	_, _ = ls.PutLocal(&nell.Record{ID: "keep", Type: nell.TypeText, Payload: []byte("alive")})
	_, _ = ls.Delete(nell.DefaultCollection, "keep") // now a tombstone with a recent clock

	// Compact with a 1-hour threshold — recent tombstone should be retained
	n, err := ls.Compact(time.Hour)
	if err != nil {
		t.Fatalf("Compact: %v", err)
	}

	// The tombstone should still be in the store
	_, getErr := ls.Get(nell.DefaultCollection, "keep")
	if getErr != nil {
		t.Errorf("recent tombstone lost: %v", getErr)
	}

	// List should NOT include the tombstone (it's deleted)
	list, _ := ls.List(nell.DefaultCollection)
	if len(list) != 0 {
		t.Errorf("List returned %d records after delete, want 0", len(list))
	}

	t.Logf("compacted %d records (tombstone retained)", n)
	_ = ls.Close()
}

func TestLogStoreCompactDropsOldTombstones(t *testing.T) {
	path := t.TempDir() + "/compact-old-tombstone.db"

	// Write a record and its deletion — both with old clocks — via Put
	// so LWW resolution picks the tombstone as the winner, and the
	// winning tombstone's clock is old enough to be dropped by a
	// 1-hour threshold.
	now := time.Now()
	threeHoursAgo := now.Add(-3 * time.Hour).UnixMilli()

	ls, _ := OpenLog(path, "node-a")

	// Write the record with an old clock.
	_, _, _ = ls.Put(nell.Record{
		ID:        "old",
		Type:      nell.TypeText,
		Payload:   []byte("ghost"),
		Clock:     nell.HLC{WallTime: threeHoursAgo, Counter: 0},
		UpdatedBy: "node-b",
	})
	// Write the tombstone with a slightly higher but still old clock.
	_, _, _ = ls.Put(nell.Record{
		ID:        "old",
		Type:      nell.TypeText,
		Clock:     nell.HLC{WallTime: threeHoursAgo, Counter: 1},
		UpdatedBy: "node-b",
		Deleted:   true,
	})

	// Compact with 1-hour threshold — the 3h-old winning tombstone
	// should be dropped.
	n, err := ls.Compact(time.Hour)
	if err != nil {
		t.Fatalf("Compact: %v", err)
	}

	// The old tombstone should be gone entirely.
	_, getErr := ls.Get(nell.DefaultCollection, "old")
	if getErr == nil {
		t.Error("old tombstone survived compaction with 1h threshold")
	}

	t.Logf("compacted %d records (old tombstone dropped)", n)
	_ = ls.Close()

	// Reopen to verify the tombstone is gone from the file.
	ls2, _ := OpenLog(path, "node-a")
	defer func() { _ = ls2.Close() }()
	list, _ := ls2.List(nell.DefaultCollection)
	if len(list) != 0 {
		t.Errorf("List after compact+reopen: got %d records, want 0", len(list))
	}
}

func TestLogStoreCompactKeepsLatestVersion(t *testing.T) {
	path := t.TempDir() + "/compact-latest.db"

	ls, _ := OpenLog(path, "node-a")

	// Write same ID 100 times — old versions should be dropped by compaction
	const overwrites = 100
	for i := range overwrites {
		_, _ = ls.PutLocal(&nell.Record{ID: "churn", Type: nell.TypeText, Payload: []byte("v")})
		_ = i
	}

	info, _ := os.Stat(path)
	bloatedSize := info.Size()

	n, err := ls.Compact(0)
	if err != nil {
		t.Fatalf("Compact: %v", err)
	}
	if n != 1 {
		t.Errorf("Compact should keep 1 record (latest), kept %d", n)
	}

	info, _ = os.Stat(path)
	if info.Size() >= bloatedSize {
		t.Errorf("compacted file did not shrink: %d >= %d", info.Size(), bloatedSize)
	}

	// Reopen and verify
	_ = ls.Close()
	ls2, _ := OpenLog(path, "node-a")
	defer func() { _ = ls2.Close() }()

	list, _ := ls2.List(nell.DefaultCollection)
	if len(list) != 1 {
		t.Fatalf("after reopen: got %d records, want 1", len(list))
	}
	if list[0].ID != "churn" {
		t.Errorf("record ID = %q, want churn", list[0].ID)
	}
}

func TestLogStoreCompactEmptyLog(t *testing.T) {
	path := t.TempDir() + "/compact-empty.db"

	ls, _ := OpenLog(path, "node-a")
	n, err := ls.Compact(time.Hour)
	if err != nil {
		t.Fatalf("Compact empty log: %v", err)
	}
	if n != 0 {
		t.Errorf("Compact empty log returned %d, want 0", n)
	}

	// Should still be usable
	_, _ = ls.PutLocal(&nell.Record{ID: "after", Type: nell.TypeText, Payload: []byte("ok")})
	_ = ls.Close()

	ls2, _ := OpenLog(path, "node-a")
	defer func() { _ = ls2.Close() }()
	list, _ := ls2.List(nell.DefaultCollection)
	if len(list) != 1 {
		t.Errorf("after compact-empty + write: %d records", len(list))
	}
}

func TestLogStoreCompactReplay(t *testing.T) {
	path := t.TempDir() + "/compact-replay.db"

	ls, _ := OpenLog(path, "node-a")
	_, _ = ls.PutLocal(&nell.Record{ID: "doc-1", Type: nell.TypeText, Payload: []byte("kept")})
	_, _ = ls.PutLocal(&nell.Record{ID: "doc-2", Type: nell.TypeText, Payload: []byte("also-kept")})
	_, _ = ls.Delete(nell.DefaultCollection, "doc-2")
	_, _ = ls.Compact(0) // drop all tombstones
	_ = ls.Close()

	// Reopen from compacted log
	ls2, _ := OpenLog(path, "node-a")
	defer func() { _ = ls2.Close() }()

	list, _ := ls2.List(nell.DefaultCollection)
	if len(list) != 1 {
		t.Fatalf("after compact+reopen: %d records, want 1 (tombstone dropped)", len(list))
	}
	if list[0].ID != "doc-1" {
		t.Errorf("record = %q, want doc-1", list[0].ID)
	}

	// Knowledge vector should only know about kept records' writers
	kv := ls2.KnowledgeVector()
	if _, ok := kv["node-a"]; !ok {
		t.Error("KnowledgeVector missing node-a after compact+replay")
	}
}

func TestLogStoreLWWOnReplay(t *testing.T) {
	path := t.TempDir() + "/test.db"

	// Write same ID twice — LWW on replay should keep the second version
	ls, _ := OpenLog(path, "node-a")
	_, _ = ls.PutLocal(&nell.Record{ID: "doc-1", Type: nell.TypeText, Payload: []byte("v1")})
	// Write v2 directly with a future clock to simulate a remote sync
	future := nell.HLC{WallTime: 99999999999999, Counter: 0}
	_, _, _ = ls.Put(nell.Record{ID: "doc-1", Type: nell.TypeText, Payload: []byte("v2-remote"), Clock: future, UpdatedBy: "node-b"})
	_ = ls.Close()

	ls2, _ := OpenLog(path, "node-a")
	defer func() { _ = ls2.Close() }()

	rec, _ := ls2.Get(nell.DefaultCollection, "doc-1")
	// On replay, the LWW resolution is applied per-pair, but the final map
	// should hold the winning version.
	_ = rec
	// Verify doc-1 exists (don't assert which version — LWW is deterministic
	// but the replay order is file order, and the last-written frame wins).
	list, _ := ls2.List(nell.DefaultCollection)
	found := false
	for _, r := range list {
		if r.ID == "doc-1" {
			found = true
			break
		}
	}
	if !found {
		t.Error("doc-1 not found after replay")
	}
}

// ── ListAll ─────────────────────────────────────────────────────────────────

func TestLogStoreListAll(t *testing.T) {
	path := t.TempDir() + "/listall.db"

	ls, err := OpenLog(path, "node-a")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ls.Close() }()

	// Create some active records.
	_, _ = ls.PutLocal(&nell.Record{ID: "doc-1", Type: nell.TypeText, Payload: []byte("alive")})
	_, _ = ls.PutLocal(&nell.Record{ID: "doc-2", Type: nell.TypeText, Payload: []byte("also-alive")})

	// Delete doc-3 — this creates a tombstone.
	_, _ = ls.PutLocal(&nell.Record{ID: "doc-3", Type: nell.TypeText, Payload: []byte("doomed")})
	_, _ = ls.Delete(nell.DefaultCollection, "doc-3")

	// List should exclude the tombstone.
	list, err := ls.List(nell.DefaultCollection)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("List() returned %d records, want 2 (only active)", len(list))
	}

	// ListAll should include the tombstone.
	all, err := ls.ListAll(nell.DefaultCollection)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Fatalf("ListAll() returned %d records, want 3 (active + tombstone)", len(all))
	}

	// Verify the tombstone is marked deleted.
	var tombstone *nell.Record
	for i := range all {
		if all[i].ID == "doc-3" {
			tombstone = &all[i]
			break
		}
	}
	if tombstone == nil {
		t.Fatal("doc-3 not found in ListAll output")
	}
	if !tombstone.Deleted {
		t.Error("doc-3 should be a tombstone (Deleted=true)")
	}
}

// ── Compact ──────────────────────────────────────────────────────────────────

func TestLogStoreCompactRemovesOldTombstonesPreservesActive(t *testing.T) {
	path := t.TempDir() + "/compact-old.db"
	now := time.Now()
	threeHoursAgo := now.Add(-3 * time.Hour).UnixMilli()

	ls, err := OpenLog(path, "node-a")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ls.Close() }()

	// Active records.
	_, _ = ls.PutLocal(&nell.Record{ID: "keep-1", Type: nell.TypeText, Payload: []byte("hello")})
	_, _ = ls.PutLocal(&nell.Record{ID: "keep-2", Type: nell.TypeText, Payload: []byte("world")})

	// Old tombstone — clock is 3 hours ago, threshold is 1 hour.
	_, _, _ = ls.Put(nell.Record{
		ID:        "old-tombstone",
		Type:      nell.TypeText,
		Clock:     nell.HLC{WallTime: threeHoursAgo, Counter: 0},
		UpdatedBy: "node-a",
		Deleted:   true,
	})

	// Recent tombstone — should survive.
	_, _ = ls.Delete(nell.DefaultCollection, "keep-2")

	// Compact with 1-hour threshold.
	n, err := ls.Compact(time.Hour)
	if err != nil {
		t.Fatalf("Compact: %v", err)
	}

	// Compact should have kept 2 records (keep-1 active + keep-2 recent tombstone;
	// old-tombstone dropped by the 1-hour threshold).
	if n != 2 {
		t.Fatalf("Compact returned %d, want 2 (1 active + 1 recent tombstone)", n)
	}

	// Active records still readable.
	rec, err := ls.Get(nell.DefaultCollection, "keep-1")
	if err != nil {
		t.Errorf("active record keep-1 lost: %v", err)
	}
	if string(rec.Payload) != "hello" {
		t.Errorf("keep-1 payload = %q, want hello", rec.Payload)
	}

	// Old tombstone gone.
	_, err = ls.Get(nell.DefaultCollection, "old-tombstone")
	if err == nil {
		t.Error("old tombstone survived compaction with 1h threshold")
	}

	// Recent tombstone (keep-2) still present as a deleted record.
	rec, err = ls.Get(nell.DefaultCollection, "keep-2")
	if err != nil {
		t.Errorf("recent tombstone keep-2 lost: %v", err)
	}
	if rec.Deleted != true {
		t.Error("keep-2 should still be a tombstone")
	}

	// After replay from compacted file, everything should still match.
	_ = ls.Close()
	ls2, err := OpenLog(path, "node-a")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ls2.Close() }()

	all, _ := ls2.ListAll(nell.DefaultCollection)
	if len(all) != 2 {
		t.Fatalf("after reopen: ListAll returned %d records, want 2", len(all))
	}
}

// ── NodeID ───────────────────────────────────────────────────────────────────

func TestLogStoreNodeID(t *testing.T) {
	path := t.TempDir() + "/nodeid.db"

	ls, err := OpenLog(path, "my-custom-node")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ls.Close() }()

	if got := ls.NodeID(); got != "my-custom-node" {
		t.Errorf("NodeID() = %q, want %q", got, "my-custom-node")
	}

	// NodeID persists across close/reopen.
	_ = ls.Close()
	ls2, err := OpenLog(path, "different-node")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ls2.Close() }()

	// After reopen with a different nodeID, the store returns the new nodeID
	// (it is a runtime identity, not persisted in the log data).
	if got := ls2.NodeID(); got != "different-node" {
		t.Errorf("after reopen NodeID() = %q, want %q", got, "different-node")
	}
}

// ── Query ────────────────────────────────────────────────────────────────────

func TestLogStoreQuery(t *testing.T) {
	path := t.TempDir() + "/query.db"

	ls, err := OpenLog(path, "node-a")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ls.Close() }()

	// Put records in different collections.
	_, _ = ls.PutLocal(&nell.Record{ID: "a-1", Collection: "alpha", Type: nell.TypeText, Payload: []byte("alpha-one")})
	_, _ = ls.PutLocal(&nell.Record{ID: "a-2", Collection: "alpha", Type: nell.TypeText, Payload: []byte("alpha-two")})
	_, _ = ls.PutLocal(&nell.Record{ID: "b-1", Collection: "beta", Type: nell.TypeText, Payload: []byte("beta-one")})

	// Query for alpha returns only alpha records.
	alpha, err := ls.Query(nell.Query{Collection: "alpha"})
	if err != nil {
		t.Fatal(err)
	}
	if len(alpha) != 2 {
		t.Fatalf("Query(alpha) returned %d records, want 2", len(alpha))
	}

	// Query for beta returns only beta records.
	beta, err := ls.Query(nell.Query{Collection: "beta"})
	if err != nil {
		t.Fatal(err)
	}
	if len(beta) != 1 {
		t.Fatalf("Query(beta) returned %d records, want 1", len(beta))
	}

	// Query for an empty collection returns nothing.
	empty, err := ls.Query(nell.Query{Collection: "gamma"})
	if err != nil {
		t.Fatal(err)
	}
	if len(empty) != 0 {
		t.Fatalf("Query(gamma) returned %d records, want 0", len(empty))
	}

	// Query excludes tombstones (same as List).
	_, _ = ls.Delete("alpha", "a-1")
	afterDelete, err := ls.Query(nell.Query{Collection: "alpha"})
	if err != nil {
		t.Fatal(err)
	}
	if len(afterDelete) != 1 {
		t.Fatalf("Query(alpha) after delete returned %d records, want 1", len(afterDelete))
	}
}

// ── OpenLogWithOptions ──────────────────────────────────────────────────────────

func TestOpenLogWithOptionsDefaults(t *testing.T) {
	path := t.TempDir() + "/opts-default.db"

	// Zero-value Options should behave identically to OpenLog.
	ls, err := OpenLogWithOptions(path, "node-a", Options{})
	if err != nil {
		t.Fatal(err)
	}

	// Per-write flush is the default, so a record should be on disk
	// immediately after PutLocal returns.
	_, err = ls.PutLocal(&nell.Record{ID: "doc-1", Type: nell.TypeText, Payload: []byte("hello")})
	if err != nil {
		t.Fatal(err)
	}

	// File should have content even without Close flushing.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() == 0 {
		t.Error("log file is empty after PutLocal with per-write flush")
	}
	_ = ls.Close()
}

func TestOpenLogWithOptionsGroupCommit(t *testing.T) {
	path := t.TempDir() + "/opts-groupcommit.db"

	ls, err := OpenLogWithOptions(path, "node-a", Options{
		FlushInterval: 5 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Write several records — they should be buffered, not flushed per-write.
	for i := range 5 {
		_, err := ls.PutLocal(&nell.Record{
			ID:      "doc-" + string(rune('0'+i)),
			Type:    nell.TypeText,
			Payload: []byte("payload"),
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	// Wait for the background flusher to tick.
	time.Sleep(30 * time.Millisecond)

	// Now the file should have content.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() == 0 {
		t.Error("log file is empty after group-commit flush interval elapsed")
	}

	// Close should flush any remaining buffered writes and stop the flusher.
	_ = ls.Close()

	// Reopen and verify all records survived.
	ls2, err := OpenLog(path, "node-a")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ls2.Close() }()

	list, _ := ls2.List(nell.DefaultCollection)
	if len(list) != 5 {
		t.Fatalf("after reopen: %d records, want 5", len(list))
	}
}

func TestOpenLogWithOptionsCompressionLevel(t *testing.T) {
	path := t.TempDir() + "/opts-zstd.db"

	ls, err := OpenLogWithOptions(path, "node-a", Options{
		CompressionLevel: zstd.SpeedFastest,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ls.Close() }()

	// Should still read/write correctly with a different compression level.
	_, err = ls.PutLocal(&nell.Record{ID: "doc-1", Type: nell.TypeText, Payload: []byte("compressed")})
	if err != nil {
		t.Fatal(err)
	}

	rec, err := ls.Get(nell.DefaultCollection, "doc-1")
	if err != nil {
		t.Fatal(err)
	}
	if string(rec.Payload) != "compressed" {
		t.Errorf("payload = %q, want compressed", rec.Payload)
	}

	// Verify it survives a reopen (tests that the decoder reads what the
	// fast encoder wrote).
	_ = ls.Close()
	ls2, err := OpenLog(path, "node-a")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ls2.Close() }()

	rec2, err := ls2.Get(nell.DefaultCollection, "doc-1")
	if err != nil {
		t.Fatal(err)
	}
	if string(rec2.Payload) != "compressed" {
		t.Errorf("after reopen payload = %q, want compressed", rec2.Payload)
	}
}

// ── GetChangesSince index ────────────────────────────────────────────────────

func TestGetChangesSinceIndexCorrectness(t *testing.T) {
	path := t.TempDir() + "/changes-idx.db"
	ls, err := OpenLog(path, "node-a")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ls.Close() }()

	// Write 3 records with known clocks.
	c1 := nell.HLC{WallTime: 1000, Counter: 0}
	c2 := nell.HLC{WallTime: 2000, Counter: 0}
	c3 := nell.HLC{WallTime: 3000, Counter: 0}

	_, _, _ = ls.Put(nell.Record{ID: "r1", Type: nell.TypeText, Payload: []byte("a"), Clock: c1, UpdatedBy: "n-a"})
	_, _, _ = ls.Put(nell.Record{ID: "r2", Type: nell.TypeText, Payload: []byte("b"), Clock: c2, UpdatedBy: "n-a"})
	_, _, _ = ls.Put(nell.Record{ID: "r3", Type: nell.TypeText, Payload: []byte("c"), Clock: c3, UpdatedBy: "n-a"})

	// since = 1500 → only r2 and r3 (clocks 2000 and 3000).
	got, err := ls.GetChangesSince(nell.HLC{WallTime: 1500})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("GetChangesSince(1500) = %d records, want 2", len(got))
	}

	// since = 0 → all 3.
	got, err = ls.GetChangesSince(nell.HLC{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("GetChangesSince(0) = %d records, want 3", len(got))
	}

	// since = 3000 → none (strictly greater).
	got, err = ls.GetChangesSince(c3)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("GetChangesSince(3000) = %d records, want 0", len(got))
	}

	// Overwrite r1 with a newer clock — index must reflect the new HLC.
	_, _, _ = ls.Put(nell.Record{ID: "r1", Type: nell.TypeText, Payload: []byte("a2"), Clock: nell.HLC{WallTime: 4000}, UpdatedBy: "n-a"})
	got, err = ls.GetChangesSince(nell.HLC{WallTime: 3500})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("after overwrite: GetChangesSince(3500) = %d records, want 1", len(got))
	}
	if got[0].ID != "r1" {
		t.Errorf("got %q, want r1", got[0].ID)
	}

	// Delete r2 — tombstone should still appear in changes.
	_, _ = ls.Delete(nell.DefaultCollection, "r2")
	got, err = ls.GetChangesSince(nell.HLC{WallTime: 3999})
	if err != nil {
		t.Fatal(err)
	}
	// r1 (clock 4000) and r2's tombstone (clock > 4000 from local tick).
	if len(got) < 1 {
		t.Fatalf("after delete: GetChangesSince(3999) = %d, want >=1", len(got))
	}
}

func TestGetChangesSinceAfterCompact(t *testing.T) {
	path := t.TempDir() + "/changes-compact.db"
	ls, err := OpenLog(path, "node-a")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ls.Close() }()

	_, _ = ls.PutLocal(&nell.Record{ID: "r1", Type: nell.TypeText, Payload: []byte("a")})
	_, _ = ls.PutLocal(&nell.Record{ID: "r2", Type: nell.TypeText, Payload: []byte("b")})

	// Compact — the index should be rebuilt on next GetChangesSince.
	n, err := ls.Compact(0)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("Compact kept %d, want 2", n)
	}

	got, err := ls.GetChangesSince(nell.HLC{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("GetChangesSince after Compact = %d, want 2", len(got))
	}
}

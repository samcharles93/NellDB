package logstore

import (
	"fmt"
	"os"
	"sync"
	"testing"

	"github.com/samcharles93/NellDB"
)

// ── Corrupted data ───────────────────────────────────────────────────────────

func TestLogStoreEmptyFile(t *testing.T) {
	path := t.TempDir() + "/empty.db"
	// Create an empty file
	if err := os.WriteFile(path, []byte{}, 0o644); err != nil {
		t.Fatal(err)
	}
	ls, err := OpenLog(path, "node-a")
	if err != nil {
		t.Fatalf("empty file should open without error: %v", err)
	}
	_ = ls.Close()
}

func TestLogStoreTruncatedFrame(t *testing.T) {
	path := t.TempDir() + "/truncated.db"
	// Write a valid frame then truncate it
	ls, _ := OpenLog(path, "node-a")
	_, _ = ls.PutLocal(&nell.Record{ID: "valid", Type: nell.TypeText, Payload: []byte("ok")})
	_ = ls.Close()

	// Truncate the file by a few bytes
	data, _ := os.ReadFile(path)
	_ = os.WriteFile(path, data[:len(data)-5], 0o644)

	// Should open cleanly — the torn frame is ignored
	ls2, err := OpenLog(path, "node-a")
	if err != nil {
		t.Fatalf("truncated file should open: %v", err)
	}
	_ = ls2.Close()
}

func TestLogStoreCorruptHeader(t *testing.T) {
	path := t.TempDir() + "/corrupt.db"
	// Write garbage bytes that don't form a valid frame
	_ = os.WriteFile(path, []byte{0xFF, 0xFF, 0xFF, 0xFF, 0x00, 0x00, 0x00, 0x01}, 0o644)

	ls, err := OpenLog(path, "node-a")
	if err != nil {
		t.Fatalf("should handle corrupt header gracefully: %v", err)
	}
	_ = ls.Close()
}

func TestLogStoreZeroLengthFrames(t *testing.T) {
	path := t.TempDir() + "/zero.db"
	// Frame declaring zero uncompressed length (valid edge case)
	f, _ := os.Create(path)
	// Write a valid empty-record frame manually
	_, _ = f.Write([]byte{0, 0, 0, 0, 0, 0, 0, 1})                                                 // uncomp=0, comp=1
	_, _ = f.Write([]byte{0x28, 0xb5, 0x2f, 0xfd, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}) // zstd frame for empty input
	_ = f.Close()

	ls, err := OpenLog(path, "node-a")
	if err != nil {
		// May fail because empty zstd frame isn't valid JSON — that's OK
		// The test is that we don't panic or hang
		t.Logf("expected failure on zero-length frame: %v", err)
	}
	if ls != nil {
		_ = ls.Close()
	}
}

// ── Stress ───────────────────────────────────────────────────────────────────

func TestLogStoreManyRecords(t *testing.T) {
	path := t.TempDir() + "/many.db"
	ls, _ := OpenLog(path, "node-a")

	const n = 5000
	for i := range n {
		_, _ = ls.PutLocal(&nell.Record{ID: fmt.Sprintf("doc-%05d", i), Type: nell.TypeText, Payload: []byte("data")})
	}
	_ = ls.Close()

	// Replay
	ls2, _ := OpenLog(path, "node-a")
	defer func() { _ = ls2.Close() }()

	list, err := ls2.List(nell.DefaultCollection)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != n {
		t.Errorf("expected %d records after replay, got %d", n, len(list))
	}
}

func TestLogStoreManyOverwrites(t *testing.T) {
	path := t.TempDir() + "/overwrite.db"
	ls, _ := OpenLog(path, "node-a")

	// Write the same ID 1000 times — log should grow, map should stay at 1 entry
	for range 1000 {
		_, _ = ls.PutLocal(&nell.Record{ID: "single", Type: nell.TypeText, Payload: []byte("overwrite")})
	}
	_ = ls.Close()

	ls2, _ := OpenLog(path, "node-a")
	defer func() { _ = ls2.Close() }()

	list, _ := ls2.List(nell.DefaultCollection)
	if len(list) != 1 {
		t.Errorf("expected 1 record after 1000 overwrites, got %d", len(list))
	}

	// Verify log file is reasonably sized (should be compact since each record is small)
	info, _ := os.Stat(path)
	// Zstd-compressed repeated JSON: each frame should be tiny
	if info.Size() > 500_000 {
		t.Errorf("log file too large after overwrites: %d bytes", info.Size())
	}
}

// ── Close correctness ────────────────────────────────────────────────────────

func TestLogStoreDoubleClose(t *testing.T) {
	path := t.TempDir() + "/double.db"
	ls, _ := OpenLog(path, "node-a")
	_, _ = ls.PutLocal(&nell.Record{ID: "test", Type: nell.TypeText, Payload: []byte("data")})
	_ = ls.Close()
	// Second close may error — should not panic
	err := ls.Close()
	t.Logf("second close: %v", err)
}

func TestLogStoreWriteAfterClose(t *testing.T) {
	path := t.TempDir() + "/after-close.db"
	ls, _ := OpenLog(path, "node-a")
	_ = ls.Close()

	// Write after close should return an error
	_, _, err := ls.Put(nell.Record{ID: "late", Type: nell.TypeText, Payload: []byte("too late")})
	if err == nil {
		t.Error("expected error writing to closed store, got nil")
	}
}

// ── KnowledgeVector persistence ──────────────────────────────────────────────

func TestLogStoreKnowledgeVectorSurvivesRestart(t *testing.T) {
	path := t.TempDir() + "/kv.db"
	ls, _ := OpenLog(path, "node-a")
	_, _, _ = ls.Put(nell.Record{ID: "from-b", Type: nell.TypeText, Payload: []byte("remote"), Clock: nell.HLC{WallTime: 5000, Counter: 0}, UpdatedBy: "node-b"})
	_ = ls.Close()

	ls2, _ := OpenLog(path, "node-a")
	defer func() { _ = ls2.Close() }()

	kv := ls2.KnowledgeVector()
	if _, ok := kv["node-b"]; !ok {
		t.Error("KnowledgeVector lost node-b entry after restart")
	}
}

// ── Concurrency ─────────────────────────────────────────────────────────────

func TestLogStoreConcurrentPuts(t *testing.T) {
	path := t.TempDir() + "/conc.db"
	ls, _ := OpenLog(path, "node-a")
	defer func() { _ = ls.Close() }()

	var wg sync.WaitGroup
	const goroutines = 20
	for i := range goroutines {
		wg.Add(1)
		go func(seq int) {
			defer wg.Done()
			_, _ = ls.PutLocal(&nell.Record{ID: fmt.Sprintf("doc-%d", seq), Type: nell.TypeText, Payload: []byte("data")})
		}(i)
	}
	wg.Wait()

	list, _ := ls.List(nell.DefaultCollection)
	if len(list) != goroutines {
		t.Errorf("expected %d records, got %d", goroutines, len(list))
	}
}

func TestLogStoreConcurrentReadWrite(t *testing.T) {
	path := t.TempDir() + "/rw.db"
	ls, _ := OpenLog(path, "node-a")
	defer func() { _ = ls.Close() }()

	// Pre-populate
	for i := range 10 {
		_, _ = ls.PutLocal(&nell.Record{ID: fmt.Sprintf("base-%d", i), Type: nell.TypeText, Payload: []byte("base")})
	}

	var wg sync.WaitGroup
	for i := range 10 {
		wg.Add(2)
		go func(seq int) {
			defer wg.Done()
			_, _ = ls.PutLocal(&nell.Record{ID: fmt.Sprintf("new-%d", seq), Type: nell.TypeText, Payload: []byte("new")})
		}(i)
		go func() {
			defer wg.Done()
			_, _ = ls.List(nell.DefaultCollection)
		}()
	}
	wg.Wait()

	list, _ := ls.List(nell.DefaultCollection)
	if len(list) < 10 {
		t.Errorf("expected at least 10 records, got %d", len(list))
	}
}

func TestLogStoreConcurrentPutAndGetChangesSince(t *testing.T) {
	path := t.TempDir() + "/changes.db"
	ls, _ := OpenLog(path, "node-a")
	defer func() { _ = ls.Close() }()

	_, _ = ls.PutLocal(&nell.Record{ID: "anchor", Type: nell.TypeText, Payload: []byte("anchor")})

	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			_, _ = ls.PutLocal(&nell.Record{ID: "churn", Type: nell.TypeText, Payload: []byte("x")})
			_, _ = ls.GetChangesSince(nell.HLC{})
		})
	}
	wg.Wait()

	// Must still be alive and consistent
	_, err := ls.Get(nell.DefaultCollection, "anchor")
	if err != nil {
		t.Fatalf("anchor lost after concurrent churn: %v", err)
	}
}

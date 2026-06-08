// Package store provides persistent backends for the NellDB nell.Store
// interface.
package logstore

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"os"
	"sync"
	"time"

	"github.com/klauspost/compress/zstd"
	"github.com/samcharles93/NellDB"
)

// ── Frame format ──────────────────────────────────────────────────────────────
//
// Each record is stored as a length-prefixed Zstd-compressed JSON blob:
//
//   [4 bytes: uncompressed_len  (big-endian uint32)]
//   [4 bytes: compressed_len    (big-endian uint32)]
//   [compressed_len bytes: Zstd compressed JSON]
//
// On startup the file is replayed frame-by-frame to rebuild the in-memory
// index.  The format is append-only and crash-safe — a torn frame at the tail
// is ignored on replay.

// ── LogStore ──────────────────────────────────────────────────────────────────

// LogStore implements nell.Store using an append-only, Zstd-compressed binary
// log for durability and an in-memory map as a read index.  On startup the
// entire log is replayed to rebuild the index.
//
// No third-party storage engine is needed.  Zstd gives excellent compression
// ratios while staying fast enough for real-time workloads (the pure-Go
// klauspost/compress implementation is CGO-free and WASM-compatible).
type LogStore struct {
	mu      sync.Mutex
	nodeID  string
	clock   nell.HLC
	records map[string]nell.Record
	kv      nell.KnowledgeVector
	file    *os.File
	writer  *bufio.Writer
	zenc    *zstd.Encoder
	zdec    *zstd.Decoder
}

// OpenLog opens (or creates) a LogStore at the given file path.
func OpenLog(path, nodeID string) (*LogStore, error) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("log: open %s: %w", path, err)
	}

	zenc, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedDefault))
	if err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("log: zstd encoder: %w", err)
	}
	zdec, err := zstd.NewReader(nil)
	if err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("log: zstd decoder: %w", err)
	}

	ls := &LogStore{
		nodeID:  nodeID,
		clock:   nell.NewHLC(),
		records: make(map[string]nell.Record),
		kv:      make(nell.KnowledgeVector),
		file:    f,
		writer:  bufio.NewWriter(f),
		zenc:    zenc,
		zdec:    zdec,
	}

	if err := ls.replay(); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("log: replay %s: %w", path, err)
	}

	return ls, nil
}

// replay reads every frame from the log and rebuilds in-memory state.
func (ls *LogStore) replay() error {
	f, err := os.Open(ls.file.Name())
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	br := bufio.NewReader(f)
	for {
		rec, err := readFrame(br, ls.zdec)
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			break
		}
		if err != nil {
			return fmt.Errorf("frame: %w", err)
		}

		if existing, ok := ls.records[rec.ID]; ok {
			winner := nell.ResolveConflict(&existing, rec)
			ls.records[rec.ID] = *winner
		} else {
			ls.records[rec.ID] = *rec
		}

		ls.clock.Update(rec.Clock)
		ls.kv.Update(rec.UpdatedBy, rec.Clock)
	}
	return nil
}

// NodeID returns the store's node identifier.
func (ls *LogStore) NodeID() string { return ls.nodeID }

// KnowledgeVector returns a copy of the local knowledge vector.
func (ls *LogStore) KnowledgeVector() nell.KnowledgeVector {
	ls.mu.Lock()
	defer ls.mu.Unlock()
	cp := make(nell.KnowledgeVector, len(ls.kv))
	maps.Copy(cp, ls.kv)
	return cp
}

// ── nell.Store implementation ─────────────────────────────────────────────────

func (ls *LogStore) Put(incoming nell.Record) (bool, nell.Record, error) {
	ls.mu.Lock()
	defer ls.mu.Unlock()

	ls.clock.Update(incoming.Clock)

	existing, ok := ls.records[incoming.ID]
	if !ok {
		ls.records[incoming.ID] = incoming
		ls.kv.Update(incoming.UpdatedBy, incoming.Clock)
		return true, incoming, ls.append(incoming)
	}

	winner := nell.ResolveConflict(&existing, &incoming)
	ls.records[incoming.ID] = *winner
	ls.kv.Update(winner.UpdatedBy, winner.Clock)
	return winner == &incoming, *winner, ls.append(*winner)
}

func (ls *LogStore) PutLocal(rec *nell.Record) (nell.Record, error) {
	ls.mu.Lock()
	defer ls.mu.Unlock()

	ls.clock = ls.clock.Tick()
	rec.Clock = ls.clock
	rec.UpdatedBy = ls.nodeID

	ls.records[rec.ID] = *rec
	ls.kv.Update(ls.nodeID, rec.Clock)
	return *rec, ls.append(*rec)
}

func (ls *LogStore) Get(id string) (nell.Record, error) {
	ls.mu.Lock()
	defer ls.mu.Unlock()
	rec, ok := ls.records[id]
	if !ok {
		return nell.Record{}, fmt.Errorf("record %q: %w", id, nell.ErrRecordNotFound)
	}
	return rec, nil
}

func (ls *LogStore) Delete(id string) (nell.Record, error) {
	ls.mu.Lock()
	defer ls.mu.Unlock()

	rec, ok := ls.records[id]
	if !ok {
		rec = nell.Record{ID: id}
	}
	ls.clock = ls.clock.Tick()
	rec.Clock = ls.clock
	rec.UpdatedBy = ls.nodeID
	rec.Deleted = true

	ls.records[id] = rec
	ls.kv.Update(ls.nodeID, rec.Clock)
	return rec, ls.append(rec)
}

func (ls *LogStore) List() ([]nell.Record, error) {
	ls.mu.Lock()
	defer ls.mu.Unlock()
	var out []nell.Record
	for _, r := range ls.records {
		if !r.Deleted {
			out = append(out, r)
		}
	}
	return out, nil
}

func (ls *LogStore) GetChangesSince(since nell.HLC) ([]nell.Record, error) {
	ls.mu.Lock()
	defer ls.mu.Unlock()
	var out []nell.Record
	for _, r := range ls.records {
		if r.Clock.GreaterThan(since) {
			out = append(out, r)
		}
	}
	return out, nil
}

// Compact rewrites the log file to reclaim space.  It:
//   - Scans the log once, keeping only the latest version of each record ID
//   - Drops tombstones whose clock is older than tombstoneThreshold
//   - Writes surviving records to a temporary file
//   - Atomically replaces the original file via os.Rename
//
// Returns the number of records written to the compacted log.  Pass a zero
// tombstoneThreshold to drop all tombstones immediately.
//
// The store remains usable during and after compaction.  On failure the
// original log file is preserved and the temporary file is cleaned up.
func (ls *LogStore) Compact(tombstoneThreshold time.Duration) (int, error) {
	ls.mu.Lock()
	defer ls.mu.Unlock()

	// Flush any buffered writes so the on-disk log is complete.
	if err := ls.writer.Flush(); err != nil {
		return 0, fmt.Errorf("compact: flush: %w", err)
	}

	// ── Phase 1: scan the log and pick the highest-HLC frame per ID ──────
	logPath := ls.file.Name()
	f, err := os.Open(logPath)
	if err != nil {
		return 0, fmt.Errorf("compact: open for scan: %w", err)
	}

	winners := make(map[string]nell.Record)
	cutoff := time.Now().UnixMilli() - tombstoneThreshold.Milliseconds()
	br := bufio.NewReader(f)

	for {
		rec, err := readFrame(br, ls.zdec)
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			break
		}
		if err != nil {
			_ = f.Close()
			return 0, fmt.Errorf("compact: read frame: %w", err)
		}

		existing, ok := winners[rec.ID]
		if !ok || rec.Clock.GreaterThan(existing.Clock) {
			winners[rec.ID] = *rec
		}
	}
	_ = f.Close()

	// ── Phase 2: filter old tombstones ────────────────────────────────────
	keep := make(map[string]nell.Record, len(winners))
	for id, rec := range winners {
		if rec.Deleted && rec.Clock.WallTime < cutoff {
			continue
		}
		keep[id] = rec
	}

	// ── Phase 3: write compacted log to a temp file ──────────────────────
	tmpPath := logPath + ".compact"
	tmp, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return 0, fmt.Errorf("compact: create temp file: %w", err)
	}

	w := bufio.NewWriter(tmp)
	// Reuse the store-level encoder — it is stateless per-encode operation.

	for _, rec := range keep {
		raw, err := json.Marshal(rec)
		if err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmpPath)
			return 0, fmt.Errorf("compact: marshal: %w", err)
		}

		compressed := ls.zenc.EncodeAll(raw, nil)

		var header [8]byte
		binary.BigEndian.PutUint32(header[0:4], uint32(len(raw)))
		binary.BigEndian.PutUint32(header[4:8], uint32(len(compressed)))

		if _, err := w.Write(header[:]); err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmpPath)
			return 0, fmt.Errorf("compact: write header: %w", err)
		}
		if _, err := w.Write(compressed); err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmpPath)
			return 0, fmt.Errorf("compact: write data: %w", err)
		}
	}

	if err := w.Flush(); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return 0, fmt.Errorf("compact: flush temp: %w", err)
	}
	_ = tmp.Close()

	// ── Phase 4: atomically replace the original file ────────────────────
	_ = ls.file.Close()

	if err := os.Rename(tmpPath, logPath); err != nil {
		// Rename failed — reopen the original so the store stays usable.
		ls.file, _ = os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_RDWR, 0o644)
		ls.writer = bufio.NewWriter(ls.file)
		_ = os.Remove(tmpPath)
		return 0, fmt.Errorf("compact: rename: %w", err)
	}

	// ── Phase 5: reopen and update in-memory state ───────────────────────
	newFile, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return 0, fmt.Errorf("compact: reopen: %w", err)
	}
	ls.file = newFile
	ls.writer = bufio.NewWriter(newFile)

	// Update in-memory maps to reflect the compacted state.
	ls.records = keep

	ls.kv = make(nell.KnowledgeVector)
	for _, rec := range keep {
		ls.kv.Update(rec.UpdatedBy, rec.Clock)
	}

	return len(keep), nil
}

func (ls *LogStore) Close() error {
	ls.mu.Lock()
	defer ls.mu.Unlock()
	if err := ls.writer.Flush(); err != nil {
		return err
	}
	return ls.file.Close()
}

// ── frame I/O ─────────────────────────────────────────────────────────────────

func (ls *LogStore) append(rec nell.Record) error {
	raw, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	compressed := ls.zenc.EncodeAll(raw, nil)

	var header [8]byte
	binary.BigEndian.PutUint32(header[0:4], uint32(len(raw)))
	binary.BigEndian.PutUint32(header[4:8], uint32(len(compressed)))

	if _, err := ls.writer.Write(header[:]); err != nil {
		return fmt.Errorf("write header: %w", err)
	}
	if _, err := ls.writer.Write(compressed); err != nil {
		return fmt.Errorf("write data: %w", err)
	}
	return ls.writer.Flush()
}

func readFrame(r io.Reader, dec *zstd.Decoder) (*nell.Record, error) {
	var header [8]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return nil, err
	}
	uncompLen := binary.BigEndian.Uint32(header[0:4])
	compLen := binary.BigEndian.Uint32(header[4:8])

	compressed := make([]byte, compLen)
	if _, err := io.ReadFull(r, compressed); err != nil {
		return nil, err
	}

	raw, err := dec.DecodeAll(compressed, nil)
	if err != nil {
		return nil, fmt.Errorf("zstd decompress: %w", err)
	}
	if len(raw) != int(uncompLen) {
		return nil, fmt.Errorf("length mismatch: declared %d, decoded %d", uncompLen, len(raw))
	}

	var rec nell.Record
	if err := json.Unmarshal(raw, &rec); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}
	return &rec, nil
}

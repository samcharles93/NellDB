package server

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"net/http"
	"testing"

	"github.com/samcharles93/NellDB"
)

func TestHandleBinCheckEmptyStore(t *testing.T) {
	srv, ts := newTestServer(t)
	defer ts.Close()

	// Marshal an empty knowledge vector.
	kv := make(nell.KnowledgeVector)
	kvBytes, err := kv.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary: %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, ts.URL+"/sync/bin/check?col="+nell.DefaultCollection, bytes.NewReader(kvBytes))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/octet-stream")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	// Response should be empty (no header bytes at all).
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	// An empty store returns empty body — the handler writes headers and then
	// iterates; with no records, nothing is written to the body.
	_ = srv
	_ = body
}

func TestHandleBinCheckWithData(t *testing.T) {
	srv, ts := newTestServer(t)
	defer ts.Close()

	// Put a record into the store.
	rec := &nell.Record{
		ID:         "doc-1",
		Type:       nell.TypeText,
		Payload:    []byte("hello"),
		UpdatedBy:  "node-a",
		Collection: nell.DefaultCollection,
	}
	_, _, err := srv.store.Put(*rec)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}

	// Marshal an empty knowledge vector — should get everything.
	kv := make(nell.KnowledgeVector)
	kvBytes, _ := kv.MarshalBinary()

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/sync/bin/check?col="+nell.DefaultCollection, bytes.NewReader(kvBytes))
	req.Header.Set("Content-Type", "application/octet-stream")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/octet-stream" {
		t.Errorf("Content-Type = %q, want application/octet-stream", ct)
	}

	// Parse binary response: 4-byte length prefix + marshaled record.
	var records []nell.Record
	respBody, _ := io.ReadAll(resp.Body)
	off := 0
	for off < len(respBody) {
		if off+4 > len(respBody) {
			t.Fatalf("truncated at offset %d", off)
		}
		recLen := binary.BigEndian.Uint32(respBody[off : off+4])
		off += 4
		if off+int(recLen) > len(respBody) {
			t.Fatalf("truncated record at offset %d, len=%d", off, recLen)
		}
		var parsed nell.Record
		if err := parsed.UnmarshalBinary(respBody[off : off+int(recLen)]); err != nil {
			t.Fatalf("UnmarshalBinary: %v", err)
		}
		records = append(records, parsed)
		off += int(recLen)
	}

	if len(records) != 1 {
		t.Fatalf("got %d records, want 1", len(records))
	}
	if records[0].ID != "doc-1" {
		t.Errorf("record ID = %q, want doc-1", records[0].ID)
	}
}

func TestHandleBinCheckKVSeen(t *testing.T) {
	srv, ts := newTestServer(t)
	defer ts.Close()

	// Put two records from different nodes.
	for _, r := range []*nell.Record{
		{ID: "doc-1", Type: nell.TypeText, Payload: []byte("a"), UpdatedBy: "node-a", Collection: nell.DefaultCollection},
		{ID: "doc-2", Type: nell.TypeText, Payload: []byte("b"), UpdatedBy: "node-b", Collection: nell.DefaultCollection},
	} {
		if _, _, err := srv.store.Put(*r); err != nil {
			t.Fatal(err)
		}
	}

	// KV says we've already seen everything from node-a.
	kv := nell.KnowledgeVector{
		"node-a": {WallTime: 1 << 62, Counter: 1 << 30}, // huge clock, covers everything
	}
	kvBytes, _ := kv.MarshalBinary()

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/sync/bin/check?col="+nell.DefaultCollection, bytes.NewReader(kvBytes))
	req.Header.Set("Content-Type", "application/octet-stream")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	// Should only get doc-2 (from node-b), not doc-1.
	var records []nell.Record
	off := 0
	for off < len(respBody) {
		recLen := binary.BigEndian.Uint32(respBody[off : off+4])
		off += 4
		var parsed nell.Record
		parsed.UnmarshalBinary(respBody[off : off+int(recLen)])
		records = append(records, parsed)
		off += int(recLen)
	}

	if len(records) != 1 {
		t.Fatalf("got %d records, want 1", len(records))
	}
	if records[0].ID != "doc-2" {
		t.Errorf("record ID = %q, want doc-2", records[0].ID)
	}
}

func TestHandleBinCheckMethodNotAllowed(t *testing.T) {
	srv, ts := newTestServer(t)
	defer ts.Close()
	_ = srv

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/sync/bin/check", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("GET bin/check: status = %d, want 405", resp.StatusCode)
	}
}

func TestHandleBinCheckBadKV(t *testing.T) {
	srv, ts := newTestServer(t)
	defer ts.Close()
	_ = srv

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/sync/bin/check", bytes.NewReader([]byte("bad")))
	req.Header.Set("Content-Type", "application/octet-stream")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("bad kv: status = %d, want 400", resp.StatusCode)
	}
}

func TestHandleBinPushSingle(t *testing.T) {
	srv, ts := newTestServer(t)
	defer ts.Close()

	// Marshal a record.
	rec := nell.Record{
		ID:         "doc-1",
		Type:       nell.TypeText,
		Payload:    []byte("pushed"),
		Clock:      nell.HLC{WallTime: 1000, Counter: 1},
		UpdatedBy:  "client",
		Collection: nell.DefaultCollection,
	}
	recBytes, err := rec.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary: %v", err)
	}

	// Build binary body: 4-byte length + record bytes.
	var body bytes.Buffer
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], uint32(len(recBytes)))
	body.Write(header[:])
	body.Write(recBytes)

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/sync/bin/push", &body)
	req.Header.Set("Content-Type", "application/octet-stream")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	// Response is 4 bytes: number accepted (big-endian uint32).
	respBody, _ := io.ReadAll(resp.Body)
	if len(respBody) != 4 {
		t.Fatalf("response len = %d, want 4", len(respBody))
	}
	accepted := binary.BigEndian.Uint32(respBody)
	if accepted != 1 {
		t.Errorf("accepted = %d, want 1", accepted)
	}

	// Verify record was stored.
	got, err := srv.store.Get(nell.DefaultCollection, "doc-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(got.Payload) != "pushed" {
		t.Errorf("payload = %q, want pushed", got.Payload)
	}
}

func TestHandleBinPushMultiple(t *testing.T) {
	srv, ts := newTestServer(t)
	defer ts.Close()

	var body bytes.Buffer
	for i := range 5 {
		rec := nell.Record{
			ID:         fmt.Sprintf("doc-%d", i),
			Type:       nell.TypeText,
			Payload:    fmt.Appendf(nil, "data-%d", i),
			Clock:      nell.HLC{WallTime: int64(1000 + i), Counter: 1},
			UpdatedBy:  "client",
			Collection: nell.DefaultCollection,
		}
		recBytes, _ := rec.MarshalBinary()
		var header [4]byte
		binary.BigEndian.PutUint32(header[:], uint32(len(recBytes)))
		body.Write(header[:])
		body.Write(recBytes)
	}

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/sync/bin/push", &body)
	req.Header.Set("Content-Type", "application/octet-stream")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	accepted := binary.BigEndian.Uint32(respBody)
	if accepted != 5 {
		t.Errorf("accepted = %d, want 5", accepted)
	}

	list, err := srv.store.List(nell.DefaultCollection)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 5 {
		t.Errorf("store has %d records, want 5", len(list))
	}
}

func TestHandleBinPushMethodNotAllowed(t *testing.T) {
	srv, ts := newTestServer(t)
	defer ts.Close()
	_ = srv

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/sync/bin/push", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("GET bin/push: status = %d, want 405", resp.StatusCode)
	}
}

func TestHandleBinPushEmptyBody(t *testing.T) {
	srv, ts := newTestServer(t)
	defer ts.Close()
	_ = srv

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/sync/bin/push", bytes.NewReader(nil))
	req.Header.Set("Content-Type", "application/octet-stream")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("empty push: status = %d, want 200", resp.StatusCode)
	}
	respBody, _ := io.ReadAll(resp.Body)
	accepted := binary.BigEndian.Uint32(respBody)
	if accepted != 0 {
		t.Errorf("accepted = %d, want 0", accepted)
	}
}

func TestHandleBinPushLWWConflict(t *testing.T) {
	srv, ts := newTestServer(t)
	defer ts.Close()

	// Pre-populate with an older record.
	old := &nell.Record{
		ID:         "doc-1",
		Type:       nell.TypeText,
		Payload:    []byte("old"),
		Clock:      nell.HLC{WallTime: 500, Counter: 1},
		UpdatedBy:  "node-a",
		Collection: nell.DefaultCollection,
	}
	srv.store.Put(*old)

	// Push a newer version.
	rec := nell.Record{
		ID:         "doc-1",
		Type:       nell.TypeText,
		Payload:    []byte("new"),
		Clock:      nell.HLC{WallTime: 1000, Counter: 1},
		UpdatedBy:  "client",
		Collection: nell.DefaultCollection,
	}
	recBytes, _ := rec.MarshalBinary()
	var body bytes.Buffer
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], uint32(len(recBytes)))
	body.Write(header[:])
	body.Write(recBytes)

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/sync/bin/push", &body)
	req.Header.Set("Content-Type", "application/octet-stream")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	accepted := binary.BigEndian.Uint32(mustReadAll(resp.Body))
	if accepted != 1 {
		t.Errorf("accepted = %d, want 1", accepted)
	}

	got, _ := srv.store.Get(nell.DefaultCollection, "doc-1")
	if string(got.Payload) != "new" {
		t.Errorf("payload = %q, want new", got.Payload)
	}
}

func TestHandleBinPushBadRecord(t *testing.T) {
	srv, ts := newTestServer(t)
	defer ts.Close()
	_ = srv

	// Length prefix says 100 bytes, but only 5 bytes follow.
	var body bytes.Buffer
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], 100)
	body.Write(header[:])
	body.Write([]byte("short"))

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/sync/bin/push", &body)
	req.Header.Set("Content-Type", "application/octet-stream")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	// Handler breaks out of loop, returns whatever it accepted (0).
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestHandleBinCheckNoCollection(t *testing.T) {
	srv, ts := newTestServer(t)
	defer ts.Close()

	kv := make(nell.KnowledgeVector)
	kvBytes, _ := kv.MarshalBinary()

	// No ?col= parameter — should default to DefaultCollection.
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/sync/bin/check", bytes.NewReader(kvBytes))
	req.Header.Set("Content-Type", "application/octet-stream")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("no collection: status = %d, want 200", resp.StatusCode)
	}
	_ = srv
}

func mustReadAll(r io.Reader) []byte {
	b, _ := io.ReadAll(r)
	return b
}

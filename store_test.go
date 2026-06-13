package nell

import (
	"testing"
	"time"
)

func TestMemoryStorePutGet(t *testing.T) {
	s := NewMemoryStore("test-node")

	rec, err := s.PutLocal(&Record{ID: "doc-1", Type: TypeText, Payload: []byte("hello")})
	if err != nil {
		t.Fatal(err)
	}
	if rec.Clock.WallTime == 0 {
		t.Error("clock not set")
	}
	if rec.UpdatedBy != "test-node" {
		t.Errorf("UpdatedBy = %q, want test-node", rec.UpdatedBy)
	}

	got, err := s.Get(DefaultCollection, "doc-1")
	if err != nil {
		t.Fatal(err)
	}
	if string(got.Payload) != "hello" {
		t.Errorf("payload = %q, want hello", got.Payload)
	}
}

func TestMemoryStoreDelete(t *testing.T) {
	s := NewMemoryStore("test-node")

	_, _ = s.PutLocal(&Record{ID: "doc-1", Type: TypeText, Payload: []byte("hello")})

	del, err := s.Delete(DefaultCollection, "doc-1")
	if err != nil {
		t.Fatal(err)
	}
	if !del.Deleted {
		t.Error("record not tombstoned")
	}

	list, _ := s.List(DefaultCollection)
	for _, r := range list {
		if r.ID == "doc-1" {
			t.Error("deleted record appeared in List")
		}
	}
}

func TestMemoryStoreLWW(t *testing.T) {
	s := NewMemoryStore("node-a")

	// Write from node-a
	_, _ = s.PutLocal(&Record{ID: "doc-1", Type: TypeText, Payload: []byte("v1")})

	// Simulate a remote write with a clock far in the future
	futureClock := HLC{WallTime: time.Now().Add(24 * time.Hour).UnixMilli(), Counter: 0}
	remote := Record{
		ID:        "doc-1",
		Type:      TypeText,
		Payload:   []byte("v2-remote"),
		Clock:     futureClock,
		UpdatedBy: "node-b",
	}
	accepted, current, err := s.Put(remote)
	if err != nil {
		t.Fatal(err)
	}
	if !accepted {
		t.Error("higher clock should be accepted")
	}
	if string(current.Payload) != "v2-remote" {
		t.Errorf("payload = %q, want v2-remote", current.Payload)
	}
}

func TestMemoryStoreGetChangesSince(t *testing.T) {
	s := NewMemoryStore("test-node")

	// Write one record, capture the clock, write another
	r1, _ := s.PutLocal(&Record{ID: "doc-1", Type: TypeText, Payload: []byte("a")})
	time.Sleep(2 * time.Millisecond)
	r2, _ := s.PutLocal(&Record{ID: "doc-2", Type: TypeText, Payload: []byte("b")})

	// r1.Clock < r2.Clock (because of the sleep)
	// Asking for changes since r1.Clock should return doc-2 only
	changes, err := s.GetChangesSince(r1.Clock)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 {
		t.Fatalf("got %d changes (want 1). r1=%v r2=%v", len(changes), r1.Clock, r2.Clock)
	}
	if changes[0].ID != "doc-2" {
		t.Errorf("got %s, want doc-2", changes[0].ID)
	}
}

func TestHLCClock(t *testing.T) {
	h := NewHLC()
	if h.WallTime == 0 {
		t.Error("WallTime not initialised")
	}

	beforeTick := h
	h2 := h.Tick()
	if !h2.GreaterThan(beforeTick) {
		t.Error("tick should produce a greater clock than before the tick")
	}
}

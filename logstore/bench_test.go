package logstore

import (
	"testing"

	"github.com/samcharles93/NellDB"
)

// BenchmarkPutLocal measures single-record write throughput with the current
// per-write-flush append path.
func BenchmarkPutLocal(b *testing.B) {
	path := b.TempDir() + "/bench.db"
	ls, err := OpenLog(path, "node-bench")
	if err != nil {
		b.Fatal(err)
	}
	defer func() { _ = ls.Close() }()

	b.ResetTimer()
	b.ReportAllocs()
	for i := range b.N {
		_, err := ls.PutLocal(&nell.Record{
			ID:      "doc-" + itoa(i),
			Type:    nell.TypeText,
			Payload: []byte("benchmark payload"),
		})
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkPutSequentialSameID measures overwrites of a single record — the
// worst case for log growth per logical record.
func BenchmarkPutSequentialSameID(b *testing.B) {
	path := b.TempDir() + "/bench-same.db"
	ls, err := OpenLog(path, "node-bench")
	if err != nil {
		b.Fatal(err)
	}
	defer func() { _ = ls.Close() }()

	b.ResetTimer()
	b.ReportAllocs()
	for i := range b.N {
		_, err := ls.PutLocal(&nell.Record{
			ID:      "single-doc",
			Type:    nell.TypeText,
			Payload: []byte("benchmark payload"),
		})
		_ = i
		if err != nil {
			b.Fatal(err)
		}
	}
}

// itoa is a small stack-friendly int->string for benchmark record IDs.
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	return string(buf[pos:])
}
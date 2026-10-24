package mobileapp

import (
	"strconv"
	"testing"
	"time"
)

// BenchmarkWriteOnceRecordAtCapacity is one applied write once the record
// holds writeOnceMax ids none of which has expired: a day's busiest stretch,
// every write within writeOnceKeep of the others.
func BenchmarkWriteOnceRecordAtCapacity(b *testing.B) {
	g := newWriteOnce()
	now := time.Unix(1000, 0)
	g.now = func() time.Time { return now }
	for i := 0; i < writeOnceMax; i++ {
		now = now.Add(time.Microsecond)
		g.remember("m"+strconv.Itoa(i%200), "w"+strconv.Itoa(i))
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		now = now.Add(time.Microsecond)
		g.remember("m"+strconv.Itoa(i%200), "x"+strconv.Itoa(i))
	}
}

package upstream

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func BenchmarkOutboxEnqueuePrices1000(b *testing.B) { benchmarkOutboxEnqueuePrices(b, 1000) }
func BenchmarkOutboxEnqueuePrices10000(b *testing.B) { benchmarkOutboxEnqueuePrices(b, 10000) }

func benchmarkOutboxEnqueuePrices(b *testing.B, count int) {
	entries := benchmarkPrices(count)
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		path := filepath.Join(b.TempDir(), "state.json")
		outbox, err := NewOutbox(path)
		if err != nil { b.Fatal(err) }
		accepted, _, err := outbox.EnqueuePrices("west", entries, count)
		if err != nil { b.Fatal(err) }
		if accepted != count { b.Fatalf("accepted=%d want=%d", accepted, count) }
	}
}

func benchmarkPrices(count int) []PriceIngest {
	observedAt := time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC)
	entries := make([]PriceIngest, count)
	for index := range entries {
		price := int64(1000 + index)
		entries[index] = PriceIngest{
			ObservedAt: observedAt, LocationID: 3005, ItemKey: fmt.Sprintf("T4_ITEM_%d", index%1000),
			Quality: int16(index%5 + 1), SellPriceMin: &price,
		}
	}
	return entries
}

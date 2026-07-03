package localdb

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"albion-market-data/collector/internal/domain"
)

func BenchmarkAppendOrders1000(b *testing.B) { benchmarkAppendOrders(b, 1000) }
func BenchmarkAppendOrders10000(b *testing.B) { benchmarkAppendOrders(b, 10000) }

func benchmarkAppendOrders(b *testing.B, count int) {
	orders := benchmarkOrders(count)
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		store, err := New(filepath.Join(b.TempDir(), "market-state.json"))
		if err != nil { b.Fatal(err) }
		written, duplicates, err := store.AppendOrders(context.Background(), orders)
		if err != nil { b.Fatal(err) }
		if written != count || duplicates != 0 { b.Fatalf("written=%d duplicates=%d", written, duplicates) }
	}
}

func benchmarkOrders(count int) []domain.NormalizedMarketOrder {
	capturedAt := time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC)
	expiresAt := capturedAt.Add(30 * 24 * time.Hour)
	orders := make([]domain.NormalizedMarketOrder, count)
	for index := range orders {
		id := int64(index + 1)
		orders[index] = domain.NormalizedMarketOrder{
			SchemaVersion: domain.NormalizedSchemaVersion, Kind: "market-order", Source: "benchmark", Server: "west",
			CapturedAt: capturedAt, OrderID: id, Item: domain.ItemDimension{ID: fmt.Sprintf("T4_ITEM_%d", index%1000)},
			Location: domain.LocationDimension{ID: "3005", Name: "Caerleon"}, Quality: domain.QualityDimension{ID: 1, Name: "Normal"},
			AuctionType: "offer", Side: "sell", UnitPrice: 1000 + id, Amount: 1, ExpiresAt: expiresAt,
			DedupeKey: fmt.Sprintf("order-%d", id),
		}
	}
	return orders
}

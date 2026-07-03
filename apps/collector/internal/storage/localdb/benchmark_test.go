package localdb

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"albion-market-data/collector/internal/domain"
	"albion-market-data/collector/internal/storage/queryjsonl"
)

func BenchmarkAppendOrders1000(b *testing.B) {
	benchmarkAppendOrders(b, 1000)
}

func BenchmarkAppendOrders10000(b *testing.B) {
	benchmarkAppendOrders(b, 10000)
}

func BenchmarkReadPrices1000(b *testing.B) {
	benchmarkReadPrices(b, 1000)
}

func BenchmarkReadPrices10000(b *testing.B) {
	benchmarkReadPrices(b, 10000)
}

func BenchmarkAppendHistory68Buckets(b *testing.B) {
	history := benchmarkHistory(68)
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		store, err := New(filepath.Join(b.TempDir(), "market-state.json"))
		if err != nil {
			b.Fatal(err)
		}
		stored, err := store.AppendHistory(context.Background(), history)
		if err != nil {
			b.Fatal(err)
		}
		if !stored {
			b.Fatal("history was not stored")
		}
	}
}

func BenchmarkReadHistory68Buckets(b *testing.B) {
	store, err := New(filepath.Join(b.TempDir(), "market-state.json"))
	if err != nil {
		b.Fatal(err)
	}
	if _, err := store.AppendHistory(context.Background(), benchmarkHistory(68)); err != nil {
		b.Fatal(err)
	}
	filter := queryjsonl.HistoryFilter{ItemID: "T4_HISTORY", Limit: 100}
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		rows, err := store.ListHistories(context.Background(), filter)
		if err != nil {
			b.Fatal(err)
		}
		if len(rows) != 1 || len(rows[0].History) != 68 {
			b.Fatalf("rows=%d", len(rows))
		}
	}
}

func benchmarkAppendOrders(b *testing.B, count int) {
	orders := benchmarkOrders(count)
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		store, err := New(filepath.Join(b.TempDir(), "market-state.json"))
		if err != nil {
			b.Fatal(err)
		}
		written, duplicates, err := store.AppendOrders(context.Background(), orders)
		if err != nil {
			b.Fatal(err)
		}
		if written != count || duplicates != 0 {
			b.Fatalf("written=%d duplicates=%d", written, duplicates)
		}
	}
}

func benchmarkReadPrices(b *testing.B, count int) {
	store, err := New(filepath.Join(b.TempDir(), "market-state.json"))
	if err != nil {
		b.Fatal(err)
	}
	if _, _, err := store.AppendOrders(context.Background(), benchmarkOrders(count)); err != nil {
		b.Fatal(err)
	}
	filter := queryjsonl.OrderFilter{Limit: count}
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		rows, err := store.ListOrders(context.Background(), filter)
		if err != nil {
			b.Fatal(err)
		}
		if len(rows) == 0 {
			b.Fatal("no rows returned")
		}
	}
}

func benchmarkOrders(count int) []domain.NormalizedMarketOrder {
	capturedAt := time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC)
	expiresAt := capturedAt.Add(30 * 24 * time.Hour)
	orders := make([]domain.NormalizedMarketOrder, count)
	for index := range orders {
		id := int64(index + 1)
		orders[index] = domain.NormalizedMarketOrder{
			SchemaVersion: domain.NormalizedSchemaVersion,
			Kind:          "market-order",
			Source:        "benchmark",
			Server:        "west",
			CapturedAt:    capturedAt,
			OrderID:       id,
			Item:          domain.ItemDimension{ID: fmt.Sprintf("T4_ITEM_%d", index%1000)},
			Location:      domain.LocationDimension{ID: "3005", Name: "Caerleon"},
			Quality:       domain.QualityDimension{ID: 1, Name: "Normal"},
			AuctionType:   "offer",
			Side:          "sell",
			UnitPrice:     1000 + id,
			Amount:        1,
			ExpiresAt:     expiresAt,
			DedupeKey:     fmt.Sprintf("order-%d", id),
		}
	}
	return orders
}

func benchmarkHistory(bucketCount int) domain.NormalizedHistory {
	capturedAt := time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC)
	points := make([]domain.NormalizedHistoryPoint, bucketCount)
	var soldUnits int64
	var totalSilver int64
	for index := range points {
		count := int64(index + 1)
		total := count * 1000
		points[index] = domain.NormalizedHistoryPoint{
			Timestamp:        capturedAt.Add(-time.Duration(index) * 6 * time.Hour),
			ItemCount:        count,
			TotalSilver:      total,
			AverageUnitPrice: 1000,
		}
		soldUnits += count
		totalSilver += total
	}
	return domain.NormalizedHistory{
		SchemaVersion: domain.NormalizedSchemaVersion,
		Kind:          "market-history",
		Source:        "benchmark",
		Server:        "west",
		Item:          domain.ItemDimension{ID: "T4_HISTORY", Name: "History item"},
		Location:      domain.LocationDimension{ID: "3005", Name: "Caerleon"},
		Quality:       domain.QualityDimension{ID: 1, Name: "Normal"},
		Period:        "days",
		CapturedAt:    capturedAt,
		Summary: domain.NormalizedHistorySummary{
			SoldUnits:                soldUnits,
			ActiveBuckets:            bucketCount,
			TotalSilver:              totalSilver,
			WeightedAverageUnitPrice: 1000,
		},
		History:   points,
		DedupeKey: fmt.Sprintf("history-%d", bucketCount),
	}
}

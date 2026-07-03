package normalizedjsonl

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"albion-market-data/collector/internal/domain"
)

func TestAppendOrdersBatchesAndDeduplicatesWithinInput(t *testing.T) {
	directory := t.TempDir()
	store, err := NewStore(directory)
	if err != nil {
		t.Fatal(err)
	}
	orders := testOrders(3)
	orders = append(orders, orders[1])
	written, duplicates, err := store.AppendOrders(context.Background(), orders)
	if err != nil {
		t.Fatal(err)
	}
	if written != 3 || duplicates != 1 {
		t.Fatalf("written=%d duplicates=%d", written, duplicates)
	}
	path := filepath.Join(directory, "market-orders-2026-07-03.jsonl")
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	lines := 0
	for scanner.Scan() {
		lines++
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if lines != 3 {
		t.Fatalf("lines=%d", lines)
	}

	reloaded, err := NewStore(directory)
	if err != nil {
		t.Fatal(err)
	}
	written, duplicates, err = reloaded.AppendOrders(context.Background(), testOrders(3))
	if err != nil {
		t.Fatal(err)
	}
	if written != 0 || duplicates != 3 {
		t.Fatalf("after restart written=%d duplicates=%d", written, duplicates)
	}
}

func BenchmarkAppendOrders1000(b *testing.B) {
	benchmarkAppendOrders(b, 1000)
}

func BenchmarkAppendOrders10000(b *testing.B) {
	benchmarkAppendOrders(b, 10000)
}

func benchmarkAppendOrders(b *testing.B, count int) {
	orders := testOrders(count)
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		directory := b.TempDir()
		store, err := NewStore(directory)
		if err != nil {
			b.Fatal(err)
		}
		if _, _, err := store.AppendOrders(context.Background(), orders); err != nil {
			b.Fatal(err)
		}
	}
}

func testOrders(count int) []domain.NormalizedMarketOrder {
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
			Item:          domain.ItemDimension{ID: fmt.Sprintf("T4_ITEM_%d", index%100)},
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

package normalization

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"albion-market-data/collector/internal/catalog"
	"albion-market-data/collector/internal/domain"
)

func BenchmarkNormalizeOrders1000(b *testing.B) {
	benchmarkNormalizeOrders(b, 1000)
}

func BenchmarkNormalizeOrders10000(b *testing.B) {
	benchmarkNormalizeOrders(b, 10000)
}

func benchmarkNormalizeOrders(b *testing.B, count int) {
	service := benchmarkService(b)
	upload := benchmarkUpload(count)
	capturedAt := time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC)
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		orders, err := service.NormalizeOrders("benchmark", "west", capturedAt, upload)
		if err != nil {
			b.Fatal(err)
		}
		if len(orders) != count {
			b.Fatalf("orders=%d want=%d", len(orders), count)
		}
	}
}

func benchmarkService(b *testing.B) *Service {
	b.Helper()
	directory := b.TempDir()
	itemsPath := filepath.Join(directory, "items.txt")
	marketsPath := filepath.Join(directory, "markets.json")
	var items string
	for index := 0; index < 1000; index++ {
		items += fmt.Sprintf("%d: T4_ITEM_%d : Benchmark item %d\n", index+1, index, index)
	}
	if err := os.WriteFile(itemsPath, []byte(items), 0o600); err != nil {
		b.Fatal(err)
	}
	markets := `{"schemaVersion":1,"markets":[{"key":"caerleon","name":"Caerleon","type":"regular","cityLocationId":"3005","marketLocationId":"3005","enabled":true}]}`
	if err := os.WriteFile(marketsPath, []byte(markets), 0o600); err != nil {
		b.Fatal(err)
	}
	loaded, err := catalog.Load(itemsPath, marketsPath)
	if err != nil {
		b.Fatal(err)
	}
	service, err := NewService(loaded, &memoryStore{})
	if err != nil {
		b.Fatal(err)
	}
	return service
}

func benchmarkUpload(count int) domain.MarketOrdersUpload {
	orders := make([]*domain.MarketOrder, count)
	for index := range orders {
		orders[index] = &domain.MarketOrder{
			ID:               int64(index + 1),
			ItemTypeID:       fmt.Sprintf("T4_ITEM_%d", index%1000),
			ItemGroupTypeID:  "T4_ITEM",
			LocationID:       "3005",
			QualityLevel:     uint8(index%5 + 1),
			EnchantmentLevel: 0,
			UnitPriceSilver:  int64(1000+index) * 10000,
			Amount:           1,
			AuctionType:      "offer",
			Expires:          "2026-08-02T12:00:00Z",
		}
	}
	return domain.MarketOrdersUpload{Orders: orders}
}

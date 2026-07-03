package httpingest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"albion-market-data/collector/internal/catalog"
	"albion-market-data/collector/internal/domain"
	"albion-market-data/collector/internal/normalization"
)

func BenchmarkCaptureOrders1000(b *testing.B) {
	benchmarkCaptureOrders(b, 1000)
}

func BenchmarkCaptureOrders10000(b *testing.B) {
	benchmarkCaptureOrders(b, 10000)
}

func benchmarkCaptureOrders(b *testing.B, count int) {
	body, err := json.Marshal(benchmarkOrdersUpload(count))
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		b.StopTimer()
		rawStore := &fakeRawStore{}
		normalizedStore := &fakeNormalizedStore{}
		handler, err := NewHandler("west", rawStore, benchmarkNormalizer(b, normalizedStore), nil, nil, log.New(io.Discard, "", 0))
		if err != nil {
			b.Fatal(err)
		}
		request := httptest.NewRequest(http.MethodPost, "/marketorders.ingest", bytes.NewReader(body))
		response := httptest.NewRecorder()
		b.StartTimer()
		handler.ServeHTTP(response, request)
		b.StopTimer()
		if response.Code != http.StatusOK {
			b.Fatalf("status=%d body=%s", response.Code, response.Body.String())
		}
		if len(normalizedStore.orders) != count {
			b.Fatalf("orders=%d want=%d", len(normalizedStore.orders), count)
		}
		b.StartTimer()
	}
}

func benchmarkNormalizer(b *testing.B, store *fakeNormalizedStore) *normalization.Service {
	b.Helper()
	directory := b.TempDir()
	itemsPath := filepath.Join(directory, "items.txt")
	marketsPath := filepath.Join(directory, "markets.json")
	if err := os.WriteFile(itemsPath, []byte("1: T4_BAG : Adept's Bag\n"), 0o600); err != nil {
		b.Fatal(err)
	}
	if err := os.WriteFile(marketsPath, []byte(`{"schemaVersion":1,"markets":[{"key":"caerleon","name":"Caerleon","type":"regular","cityLocationId":"3005","marketLocationId":"3005","enabled":true}]}`), 0o600); err != nil {
		b.Fatal(err)
	}
	loaded, err := catalog.Load(itemsPath, marketsPath)
	if err != nil {
		b.Fatal(err)
	}
	service, err := normalization.NewService(loaded, store)
	if err != nil {
		b.Fatal(err)
	}
	return service
}

func benchmarkOrdersUpload(count int) domain.MarketOrdersUpload {
	orders := make([]*domain.MarketOrder, count)
	for index := range orders {
		orders[index] = &domain.MarketOrder{
			ID:               int64(index + 1),
			ItemTypeID:       "T4_BAG",
			ItemGroupTypeID:  "T4_BAG",
			LocationID:       "3005",
			QualityLevel:     uint8(index%5 + 1),
			EnchantmentLevel: 0,
			UnitPriceSilver:  int64(1000+index) * 10000,
			Amount:           1,
			AuctionType:      "offer",
			Expires:          "2026-08-02T12:00:00Z",
		}
	}
	if len(orders) != count {
		panic(fmt.Sprintf("orders=%d", len(orders)))
	}
	return domain.MarketOrdersUpload{Orders: orders}
}

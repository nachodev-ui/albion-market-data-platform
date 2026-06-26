package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"albion-market-data/collector/internal/catalog"
	"albion-market-data/collector/internal/domain"
	"albion-market-data/collector/internal/storage/normalizedjsonl"
	"albion-market-data/collector/internal/storage/queryjsonl"
	"albion-market-data/collector/internal/upstream"
)

func testAPICatalog(t *testing.T) *catalog.Catalog {
	t.Helper()
	directory := t.TempDir()
	items := filepath.Join(directory, "items.txt")
	markets := filepath.Join(directory, "markets.json")
	if err := os.WriteFile(items, []byte("1: T4_SWORD : Test Sword\n6826: T4_MAIN_CURSEDSTAFF_CRYSTAL@4 : Adept's Rotcaller Staff\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(markets, []byte(`{"schemaVersion":1,"markets":[{"key":"brecilien","name":"Brecilien","type":"regular","cityLocationId":"5000","marketLocationId":"5003","enabled":true},{"key":"black_market","name":"Black Market","type":"black-market","cityLocationId":"3003","marketLocationId":null,"enabled":false}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := catalog.Load(items, markets)
	if err != nil {
		t.Fatal(err)
	}
	return loaded
}

func TestHistoryEndpointFiltersNormalizedData(t *testing.T) {
	directory := t.TempDir()
	store, err := normalizedjsonl.NewStore(directory)
	if err != nil {
		t.Fatal(err)
	}
	history := domain.NormalizedHistory{
		SchemaVersion: 1, Kind: "market-history", Source: "test", Server: "west",
		Item:     domain.ItemDimension{AlbionID: 6826, ID: "T4_MAIN_CURSEDSTAFF_CRYSTAL@4"},
		Location: domain.LocationDimension{ID: "1301", Name: "Bridgewatch"},
		Quality:  domain.QualityDimension{ID: 2, Name: "Bueno"}, Period: "7-days",
		CapturedAt: time.Date(2026, 6, 22, 18, 0, 0, 0, time.UTC),
		Summary:    domain.NormalizedHistorySummary{SoldUnits: 1, ActiveBuckets: 1, TotalSilver: 904439, WeightedAverageUnitPrice: 904439},
		History:    []domain.NormalizedHistoryPoint{{Timestamp: time.Date(2026, 6, 21, 0, 0, 0, 0, time.UTC), ItemCount: 1, TotalSilver: 904439, AverageUnitPrice: 904439}},
		DedupeKey:  "history-key",
	}
	if _, err := store.AppendHistory(context.Background(), history); err != nil {
		t.Fatal(err)
	}
	repository, _ := queryjsonl.NewRepository(filepath.Clean(directory))
	handler, _ := NewHandler(repository, testAPICatalog(t))
	request := httptest.NewRequest(http.MethodGet, "/api/v1/history?itemId=T4_MAIN_CURSEDSTAFF_CRYSTAL%404&locationId=1301&quality=2&period=7-days&limit=1", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var payload struct {
		Count int                        `json:"count"`
		Data  []domain.NormalizedHistory `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Count != 1 || payload.Data[0].Summary.TotalSilver != 904439 {
		t.Fatalf("payload=%+v", payload)
	}
}

func TestPricesEndpointAggregatesActiveOrders(t *testing.T) {
	directory := t.TempDir()
	store, err := normalizedjsonl.NewStore(directory)
	if err != nil {
		t.Fatal(err)
	}
	capturedAt := time.Date(2026, 6, 22, 20, 0, 0, 0, time.UTC)
	expiresAt := time.Date(2026, 7, 22, 20, 0, 0, 0, time.UTC)
	orders := []domain.NormalizedMarketOrder{
		{
			SchemaVersion: 1, Kind: "market-order", Source: "test", Server: "west", CapturedAt: capturedAt.Add(-5 * time.Minute),
			OrderID: 10, Item: domain.ItemDimension{ID: "T4_SWORD"}, Location: domain.LocationDimension{ID: "5003", Name: "Brecilien"},
			Quality: domain.QualityDimension{ID: 1, Name: "Normal"}, AuctionType: "offer", Side: "sell", UnitPrice: 1, Amount: 1, ExpiresAt: expiresAt, DedupeKey: "stale-sell",
		},
		{
			SchemaVersion: 1, Kind: "market-order", Source: "test", Server: "west", CapturedAt: capturedAt.Add(-5 * time.Minute),
			OrderID: 11, Item: domain.ItemDimension{ID: "T4_SWORD"}, Location: domain.LocationDimension{ID: "5003", Name: "Brecilien"},
			Quality: domain.QualityDimension{ID: 1, Name: "Normal"}, AuctionType: "request", Side: "buy", UnitPrice: 999999, Amount: 1, ExpiresAt: expiresAt, DedupeKey: "stale-buy",
		},
		{
			SchemaVersion: 1, Kind: "market-order", Source: "test", Server: "west", CapturedAt: capturedAt,
			OrderID: 1, Item: domain.ItemDimension{ID: "T4_SWORD"}, Location: domain.LocationDimension{ID: "5003", Name: "Brecilien"},
			Quality: domain.QualityDimension{ID: 1, Name: "Normal"}, AuctionType: "offer", Side: "sell", UnitPrice: 1200, Amount: 1, ExpiresAt: expiresAt, DedupeKey: "sell-high",
		},
		{
			SchemaVersion: 1, Kind: "market-order", Source: "test", Server: "west", CapturedAt: capturedAt.Add(time.Minute),
			OrderID: 2, Item: domain.ItemDimension{ID: "T4_SWORD"}, Location: domain.LocationDimension{ID: "5003", Name: "Brecilien"},
			Quality: domain.QualityDimension{ID: 1, Name: "Normal"}, AuctionType: "offer", Side: "sell", UnitPrice: 1000, Amount: 1, ExpiresAt: expiresAt, DedupeKey: "sell-low",
		},
		{
			SchemaVersion: 1, Kind: "market-order", Source: "test", Server: "west", CapturedAt: capturedAt.Add(2 * time.Minute),
			OrderID: 3, Item: domain.ItemDimension{ID: "T4_SWORD"}, Location: domain.LocationDimension{ID: "5003", Name: "Brecilien"},
			Quality: domain.QualityDimension{ID: 1, Name: "Normal"}, AuctionType: "request", Side: "buy", UnitPrice: 800, Amount: 1, ExpiresAt: expiresAt, DedupeKey: "buy-low",
		},
		{
			SchemaVersion: 1, Kind: "market-order", Source: "test", Server: "west", CapturedAt: capturedAt.Add(3 * time.Minute),
			OrderID: 4, Item: domain.ItemDimension{ID: "T4_SWORD"}, Location: domain.LocationDimension{ID: "5003", Name: "Brecilien"},
			Quality: domain.QualityDimension{ID: 1, Name: "Normal"}, AuctionType: "request", Side: "buy", UnitPrice: 900, Amount: 1, ExpiresAt: expiresAt, DedupeKey: "buy-high",
		},
	}
	if _, _, err := store.AppendOrders(context.Background(), orders); err != nil {
		t.Fatal(err)
	}
	repository, err := queryjsonl.NewRepository(directory)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewHandler(repository, testAPICatalog(t))
	if err != nil {
		t.Fatal(err)
	}
	handler.now = func() time.Time { return time.Date(2026, 6, 23, 0, 0, 0, 0, time.UTC) }

	request := httptest.NewRequest(http.MethodGet, "/api/v1/prices?server=west&itemIds=T4_SWORD&marketKey=brecilien&quality=1", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var payload struct {
		Data []struct {
			SellPriceMin *int64 `json:"sellPriceMin"`
			BuyPriceMax  *int64 `json:"buyPriceMax"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Data) != 1 || payload.Data[0].SellPriceMin == nil || *payload.Data[0].SellPriceMin != 1000 || payload.Data[0].BuyPriceMax == nil || *payload.Data[0].BuyPriceMax != 900 {
		t.Fatalf("payload=%s", response.Body.String())
	}
}

func TestMarketsEndpointReturnsEnabledCatalog(t *testing.T) {
	handler, err := NewHandler(&emptyRepository{}, testAPICatalog(t))
	if err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/v1/markets", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}

	var payload struct {
		Count int                       `json:"count"`
		Data  []domain.MarketDefinition `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Count != 1 || len(payload.Data) != 1 || payload.Data[0].Key != "brecilien" {
		t.Fatalf("payload=%s", response.Body.String())
	}

	request = httptest.NewRequest(http.MethodGet, "/api/v1/markets?includeDisabled=true", nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Count != 2 {
		t.Fatalf("payload=%s", response.Body.String())
	}
}

type emptyRepository struct{}

func (*emptyRepository) ListHistories(context.Context, queryjsonl.HistoryFilter) ([]domain.NormalizedHistory, error) {
	return nil, nil
}

func (*emptyRepository) ListOrders(context.Context, queryjsonl.OrderFilter) ([]domain.NormalizedMarketOrder, error) {
	return nil, nil
}

type fakeForwarderStatus struct {
	snapshot upstream.ForwarderSnapshot
}

func (f fakeForwarderStatus) Snapshot() upstream.ForwarderSnapshot {
	return f.snapshot
}

func TestStatusEndpointIncludesForwarderObservability(t *testing.T) {
	startedAt := time.Date(2026, 6, 26, 0, 0, 0, 0, time.UTC)
	lastSuccess := startedAt.Add(30 * time.Second)
	provider := fakeForwarderStatus{snapshot: upstream.ForwarderSnapshot{
		Enabled: true,
		Running: true,
		Status:  "ok",
		Queue: upstream.QueueSnapshot{
			Depth:              12,
			Capacity:           5000,
			UtilizationPercent: 0.24,
			HighWatermark:      200,
		},
		Totals: upstream.TotalsSnapshot{
			BatchesSent:      9,
			EntriesSent:      4500,
			Retries:          2,
			RecoveredBatches: 1,
		},
		LatencyMS: upstream.LatencySnapshot{
			LastBatchMS:    75.5,
			AverageBatchMS: 82.3,
			MaxBatchMS:     120.1,
		},
		LastSuccessAt: &lastSuccess,
	}}

	handler, err := NewHandler(&emptyRepository{}, testAPICatalog(t), StatusConfig{
		ServiceName: "albion-market-data-platform",
		Environment: "test",
		StartedAt:   startedAt,
		Forwarder:   provider,
	})
	if err != nil {
		t.Fatal(err)
	}
	handler.now = func() time.Time { return startedAt.Add(90 * time.Second) }

	request := httptest.NewRequest(http.MethodGet, "/api/v1/status", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var payload statusResponse
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Status != "ok" || payload.Service != "albion-market-data-platform" || payload.Environment != "test" {
		t.Fatalf("payload=%+v", payload)
	}
	if payload.UptimeSeconds != 90 {
		t.Fatalf("uptime=%d", payload.UptimeSeconds)
	}
	if payload.Forwarder.Queue.Depth != 12 || payload.Forwarder.Totals.EntriesSent != 4500 {
		t.Fatalf("forwarder=%+v", payload.Forwarder)
	}
}

func TestStatusEndpointReportsDegradedForwarder(t *testing.T) {
	provider := fakeForwarderStatus{snapshot: upstream.ForwarderSnapshot{
		Enabled:   true,
		Running:   true,
		Status:    "degraded",
		Queue:     upstream.QueueSnapshot{Depth: 95, Capacity: 100, UtilizationPercent: 95},
		LastError: "upstream returned 503",
	}}
	handler, err := NewHandler(&emptyRepository{}, testAPICatalog(t), StatusConfig{Forwarder: provider})
	if err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/v1/status", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	var payload statusResponse
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Status != "degraded" || payload.Forwarder.Status != "degraded" {
		t.Fatalf("payload=%+v", payload)
	}
}

func TestStatusEndpointShowsDisabledForwarder(t *testing.T) {
	handler, err := NewHandler(&emptyRepository{}, testAPICatalog(t), StatusConfig{ForwarderQueueCapacity: 5000})
	if err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/v1/status", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	var payload statusResponse
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Status != "ok" || payload.Forwarder.Enabled || payload.Forwarder.Status != "disabled" {
		t.Fatalf("payload=%+v", payload)
	}
	if payload.Forwarder.Queue.Capacity != 5000 {
		t.Fatalf("queue=%+v", payload.Forwarder.Queue)
	}
}

func TestStatusEndpointTreatsTypedNilForwarderAsDisabled(t *testing.T) {
	var forwarder *upstream.Forwarder
	handler, err := NewHandler(&emptyRepository{}, testAPICatalog(t), StatusConfig{
		Forwarder:              forwarder,
		ForwarderQueueCapacity: 5000,
	})
	if err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/v1/status", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var payload statusResponse
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Forwarder.Enabled || payload.Forwarder.Status != "disabled" {
		t.Fatalf("forwarder=%+v", payload.Forwarder)
	}
}

type fakeHistoryForwarderStatus struct {
	snapshot upstream.HistoryForwarderSnapshot
}

func (f fakeHistoryForwarderStatus) Snapshot() upstream.HistoryForwarderSnapshot {
	return f.snapshot
}

func TestStatusEndpointSeparatesPriceAndHistoryForwarders(t *testing.T) {
	price := fakeForwarderStatus{snapshot: upstream.ForwarderSnapshot{
		Enabled: true,
		Running: true,
		Status:  "ok",
		Queue:   upstream.QueueSnapshot{Capacity: 5000},
		Totals:  upstream.TotalsSnapshot{EntriesSent: 25},
	}}
	history := fakeHistoryForwarderStatus{snapshot: upstream.HistoryForwarderSnapshot{
		Enabled: true,
		Running: true,
		Status:  "degraded",
		Queue:   upstream.QueueSnapshot{Depth: 95, Capacity: 100, UtilizationPercent: 95},
		Totals: upstream.HistoryTotalsSnapshot{
			EntriesSent: 8,
			BucketsSent: 544,
		},
		LastError: "upstream returned 503",
	}}

	handler, err := NewHandler(&emptyRepository{}, testAPICatalog(t), StatusConfig{
		Forwarder:                     price,
		ForwarderQueueCapacity:        5000,
		HistoryForwarder:              history,
		HistoryForwarderQueueCapacity: 100,
	})
	if err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/v1/status", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var payload statusResponse
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Status != "degraded" {
		t.Fatalf("status=%q", payload.Status)
	}
	if payload.PriceForwarder.Totals.EntriesSent != 25 || payload.Forwarder.Totals.EntriesSent != 25 {
		t.Fatalf("price forwarder=%+v legacy=%+v", payload.PriceForwarder, payload.Forwarder)
	}
	if payload.HistoryForwarder.Totals.EntriesSent != 8 || payload.HistoryForwarder.Totals.BucketsSent != 544 {
		t.Fatalf("history forwarder=%+v", payload.HistoryForwarder)
	}
}

func TestStatusEndpointShowsDisabledHistoryForwarderCapacity(t *testing.T) {
	handler, err := NewHandler(&emptyRepository{}, testAPICatalog(t), StatusConfig{
		ForwarderQueueCapacity:        5000,
		HistoryForwarderQueueCapacity: 1000,
	})
	if err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/v1/status", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	var payload statusResponse
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.HistoryForwarder.Enabled || payload.HistoryForwarder.Status != "disabled" {
		t.Fatalf("history forwarder=%+v", payload.HistoryForwarder)
	}
	if payload.HistoryForwarder.Queue.Capacity != 1000 {
		t.Fatalf("history queue=%+v", payload.HistoryForwarder.Queue)
	}
}

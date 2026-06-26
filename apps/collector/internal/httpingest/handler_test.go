package httpingest

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"albion-market-data/collector/internal/catalog"
	"albion-market-data/collector/internal/domain"
	"albion-market-data/collector/internal/normalization"
	"albion-market-data/collector/internal/upstream"
)

type fakeRawStore struct {
	events []domain.RawIngestEvent
}

func (s *fakeRawStore) AppendRaw(_ context.Context, event domain.RawIngestEvent) error {
	s.events = append(s.events, event)
	return nil
}

type fakeNormalizedStore struct {
	histories []domain.NormalizedHistory
	orders    []domain.NormalizedMarketOrder
	seen      map[string]struct{}
}

func (s *fakeNormalizedStore) AppendHistory(_ context.Context, history domain.NormalizedHistory) (bool, error) {
	if s.seen == nil {
		s.seen = map[string]struct{}{}
	}
	if _, ok := s.seen[history.DedupeKey]; ok {
		return false, nil
	}
	s.seen[history.DedupeKey] = struct{}{}
	s.histories = append(s.histories, history)
	return true, nil
}

func (s *fakeNormalizedStore) AppendOrders(_ context.Context, orders []domain.NormalizedMarketOrder) (int, int, error) {
	if s.seen == nil {
		s.seen = map[string]struct{}{}
	}
	written, duplicates := 0, 0
	for _, order := range orders {
		if _, ok := s.seen[order.DedupeKey]; ok {
			duplicates++
			continue
		}
		s.seen[order.DedupeKey] = struct{}{}
		s.orders = append(s.orders, order)
		written++
	}
	return written, duplicates, nil
}

func newTestNormalizer(t *testing.T, store *fakeNormalizedStore) *normalization.Service {
	t.Helper()
	directory := t.TempDir()
	items := filepath.Join(directory, "items.txt")
	markets := filepath.Join(directory, "markets.json")
	if err := os.WriteFile(items, []byte("6826: T4_MAIN_CURSEDSTAFF_CRYSTAL@4 : Adept's Rotcaller Staff\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(markets, []byte(`{"schemaVersion":1,"markets":[{"key":"lymhurst","name":"Lymhurst","type":"regular","cityLocationId":"1000","marketLocationId":"1002","enabled":true}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := catalog.Load(items, markets)
	if err != nil {
		t.Fatal(err)
	}
	service, err := normalization.NewService(loaded, store)
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func TestHandlerStoresRawAndNormalizedMarketHistory(t *testing.T) {
	rawStore := &fakeRawStore{}
	normalizedStore := &fakeNormalizedStore{}
	handler, err := NewHandler("west", rawStore, newTestNormalizer(t, normalizedStore), nil, nil, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	handler.now = func() time.Time { return time.Date(2026, 6, 22, 18, 51, 51, 0, time.UTC) }

	body := []byte(`{"AlbionId":6826,"LocationId":"1002","QualityLevel":2,"Timescale":1,"MarketHistories":[{"ItemAmount":1,"SilverAmount":9044390000,"Timestamp":639175968000000000}]}`)
	request := httptest.NewRequest(http.MethodPost, "/markethistories.ingest", bytes.NewReader(body))
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if len(rawStore.events) != 1 || len(normalizedStore.histories) != 1 {
		t.Fatalf("raw=%d normalized=%d", len(rawStore.events), len(normalizedStore.histories))
	}
	history := normalizedStore.histories[0]
	if history.Item.ID != "T4_MAIN_CURSEDSTAFF_CRYSTAL@4" || history.Summary.TotalSilver != 904439 {
		t.Fatalf("unexpected normalized history: %+v", history)
	}

	var result map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if result["status"] != "normalized" || result["itemId"] != "T4_MAIN_CURSEDSTAFF_CRYSTAL@4" {
		t.Fatalf("response = %v", result)
	}
}

func TestNewHandlerNormalizesTypedNilForwarders(t *testing.T) {
	rawStore := &fakeRawStore{}
	normalizedStore := &fakeNormalizedStore{}
	var priceForwarder *upstream.Forwarder
	var historyForwarder *upstream.HistoryForwarder

	handler, err := NewHandler(
		"west",
		rawStore,
		newTestNormalizer(t, normalizedStore),
		priceForwarder,
		historyForwarder,
		log.New(io.Discard, "", 0),
	)
	if err != nil {
		t.Fatal(err)
	}
	if handler.priceForwarder != nil || handler.historyForwarder != nil {
		t.Fatalf("typed nil forwarders were not normalized: price=%#v history=%#v", handler.priceForwarder, handler.historyForwarder)
	}
}

func TestHandlerNormalizesAndDeduplicatesMarketOrders(t *testing.T) {
	rawStore := &fakeRawStore{}
	normalizedStore := &fakeNormalizedStore{}
	handler, err := NewHandler("west", rawStore, newTestNormalizer(t, normalizedStore), nil, nil, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	handler.now = func() time.Time { return time.Date(2026, 6, 22, 18, 52, 0, 0, time.UTC) }
	body := []byte(`{"Orders":[{"Id":15083190320,"ItemTypeId":"T4_MAIN_CURSEDSTAFF_CRYSTAL@4","ItemGroupTypeId":"T4_MAIN_CURSEDSTAFF_CRYSTAL","LocationId":"1002","QualityLevel":2,"EnchantmentLevel":4,"UnitPriceSilver":9099970000,"Amount":1,"AuctionType":"offer","Expires":"2026-07-22T18:01:45.98371"}]}`)

	for range 2 {
		request := httptest.NewRequest(http.MethodPost, "/marketorders.ingest", bytes.NewReader(body))
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
		}
	}
	if len(rawStore.events) != 2 || len(normalizedStore.orders) != 1 {
		t.Fatalf("raw=%d orders=%d", len(rawStore.events), len(normalizedStore.orders))
	}
	if normalizedStore.orders[0].UnitPrice != 909997 {
		t.Fatalf("unit price = %d", normalizedStore.orders[0].UnitPrice)
	}
}

func TestHandlerStoresUnknownAoDataTopicAsRaw(t *testing.T) {
	rawStore := &fakeRawStore{}
	handler, err := NewHandler("west", rawStore, newTestNormalizer(t, &fakeNormalizedStore{}), nil, nil, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "/some-topic.ingest", bytes.NewBufferString(`{"value":1}`))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK || len(rawStore.events) != 1 {
		t.Fatalf("status=%d raw=%d", response.Code, len(rawStore.events))
	}
}

type fakePriceForwarder struct {
	entries []upstream.PriceIngest
}

func (f *fakePriceForwarder) Enqueue(entry upstream.PriceIngest) bool {
	f.entries = append(f.entries, entry)
	return true
}

func TestHandlerBuildsAndQueuesCurrentPriceSnapshots(t *testing.T) {
	rawStore := &fakeRawStore{}
	normalizedStore := &fakeNormalizedStore{}
	forwarder := &fakePriceForwarder{}
	handler, err := NewHandler("west", rawStore, newTestNormalizer(t, normalizedStore), forwarder, nil, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	handler.now = func() time.Time { return time.Date(2026, 6, 22, 18, 52, 0, 0, time.UTC) }
	body := []byte(`{"Orders":[
		{"Id":15083190320,"ItemTypeId":"T4_MAIN_CURSEDSTAFF_CRYSTAL@4","ItemGroupTypeId":"T4_MAIN_CURSEDSTAFF_CRYSTAL","LocationId":"1002","QualityLevel":2,"EnchantmentLevel":4,"UnitPriceSilver":9099970000,"Amount":1,"AuctionType":"offer","Expires":"2026-07-22T18:01:45.98371"},
		{"Id":15083190321,"ItemTypeId":"T4_MAIN_CURSEDSTAFF_CRYSTAL@4","ItemGroupTypeId":"T4_MAIN_CURSEDSTAFF_CRYSTAL","LocationId":"1002","QualityLevel":2,"EnchantmentLevel":4,"UnitPriceSilver":8999970000,"Amount":1,"AuctionType":"offer","Expires":"2026-07-22T18:01:45.98371"},
		{"Id":15083190322,"ItemTypeId":"T4_MAIN_CURSEDSTAFF_CRYSTAL@4","ItemGroupTypeId":"T4_MAIN_CURSEDSTAFF_CRYSTAL","LocationId":"1002","QualityLevel":2,"EnchantmentLevel":4,"UnitPriceSilver":8700000000,"Amount":1,"AuctionType":"request","Expires":"2026-07-22T18:01:45.98371"}
	]}`)
	request := httptest.NewRequest(http.MethodPost, "/marketorders.ingest", bytes.NewReader(body))
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}
	if len(forwarder.entries) != 1 {
		t.Fatalf("forwarded entries = %d", len(forwarder.entries))
	}
	entry := forwarder.entries[0]
	if entry.LocationID != 1002 || entry.ItemKey != "T4_MAIN_CURSEDSTAFF_CRYSTAL@4" || entry.Quality != 2 {
		t.Fatalf("unexpected forwarded entry: %+v", entry)
	}
	if entry.SellPriceMin == nil || *entry.SellPriceMin != 899997 {
		t.Fatalf("sell price min = %#v", entry.SellPriceMin)
	}
	if entry.BuyPriceMax == nil || *entry.BuyPriceMax != 870000 {
		t.Fatalf("buy price max = %#v", entry.BuyPriceMax)
	}
}

type fakeHistoryForwarder struct {
	entries []upstream.HistoryIngest
	accept  bool
}

func (f *fakeHistoryForwarder) Enqueue(entry upstream.HistoryIngest) bool {
	f.entries = append(f.entries, entry)
	return f.accept
}

func TestHandlerBuildsAndQueuesNormalizedHistory(t *testing.T) {
	rawStore := &fakeRawStore{}
	normalizedStore := &fakeNormalizedStore{}
	forwarder := &fakeHistoryForwarder{accept: true}
	handler, err := NewHandler(
		"west",
		rawStore,
		newTestNormalizer(t, normalizedStore),
		nil,
		forwarder,
		log.New(io.Discard, "", 0),
	)
	if err != nil {
		t.Fatal(err)
	}
	capturedAt := time.Date(2026, 6, 26, 18, 0, 0, 0, time.UTC)
	handler.now = func() time.Time { return capturedAt }

	body := []byte(`{"AlbionId":6826,"LocationId":"1002","QualityLevel":2,"Timescale":2,"MarketHistories":[{"ItemAmount":2,"SilverAmount":18088780000,"Timestamp":639175968000000000}]}`)
	request := httptest.NewRequest(http.MethodPost, "/markethistories.ingest", bytes.NewReader(body))
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}
	if len(forwarder.entries) != 1 {
		t.Fatalf("forwarded entries = %d", len(forwarder.entries))
	}
	entry := forwarder.entries[0]
	if entry.ObservedAt != capturedAt || entry.LocationID != 1002 || entry.ItemKey != "T4_MAIN_CURSEDSTAFF_CRYSTAL@4" || entry.Quality != 2 {
		t.Fatalf("unexpected forwarded entry: %+v", entry)
	}
	if len(entry.History) != 1 || entry.History[0].ItemCount != 2 {
		t.Fatalf("unexpected buckets: %+v", entry.History)
	}
	if entry.History[0].AverageUnitPrice == nil || *entry.History[0].AverageUnitPrice != 904439 {
		t.Fatalf("average price = %#v", entry.History[0].AverageUnitPrice)
	}

	var payload struct {
		Forwarded        int `json:"forwarded"`
		ForwardedBuckets int `json:"forwardedBuckets"`
		Dropped          int `json:"dropped"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Forwarded != 1 || payload.ForwardedBuckets != 1 || payload.Dropped != 0 {
		t.Fatalf("payload = %+v", payload)
	}
}

func TestBuildUpstreamHistoryEntryUsesNullPriceForInactiveBucket(t *testing.T) {
	entry, err := buildUpstreamHistoryEntry(domain.NormalizedHistory{
		Item:       domain.ItemDimension{ID: "T4_TEST"},
		Location:   domain.LocationDimension{ID: "4002"},
		Quality:    domain.QualityDimension{ID: 1},
		CapturedAt: time.Date(2026, 6, 26, 18, 0, 0, 0, time.UTC),
		History: []domain.NormalizedHistoryPoint{{
			Timestamp:        time.Date(2026, 6, 25, 0, 0, 0, 0, time.UTC),
			ItemCount:        0,
			TotalSilver:      0,
			AverageUnitPrice: 0,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(entry.History) != 1 || entry.History[0].AverageUnitPrice != nil {
		t.Fatalf("entry = %+v", entry)
	}
}

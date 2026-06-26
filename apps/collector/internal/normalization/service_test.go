package normalization

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"albion-market-data/collector/internal/catalog"
	"albion-market-data/collector/internal/domain"
)

type memoryStore struct {
	histories []domain.NormalizedHistory
	orders    []domain.NormalizedMarketOrder
	seen      map[string]struct{}
}

func (s *memoryStore) AppendHistory(_ context.Context, history domain.NormalizedHistory) (bool, error) {
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

func (s *memoryStore) AppendOrders(_ context.Context, orders []domain.NormalizedMarketOrder) (int, int, error) {
	if s.seen == nil {
		s.seen = map[string]struct{}{}
	}
	written := 0
	duplicates := 0
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

func testCatalog(t *testing.T) *catalog.Catalog {
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
	return loaded
}

func TestNormalizeOfficialHistory(t *testing.T) {
	service, err := NewService(testCatalog(t), &memoryStore{})
	if err != nil {
		t.Fatal(err)
	}
	capture := domain.CapturedHistory{
		SchemaVersion: 1,
		Source:        "aodp-http-ingest",
		Server:        "west",
		CapturedAt:    time.Date(2026, 6, 22, 18, 51, 51, 0, time.UTC),
		Payload: domain.MarketHistoriesUpload{
			AlbionID:     6826,
			LocationID:   "1002",
			QualityLevel: 2,
			Timescale:    domain.TimescaleDays,
			Histories: []*domain.MarketHistory{
				{ItemAmount: 1, SilverAmount: 9_044_390_000, Timestamp: 639_175_968_000_000_000},
				{ItemAmount: 2, SilverAmount: 18_088_770_000, Timestamp: 639_175_752_000_000_000},
			},
		},
	}

	normalized, err := service.NormalizeHistory(capture)
	if err != nil {
		t.Fatal(err)
	}
	if normalized.Item.ID != "T4_MAIN_CURSEDSTAFF_CRYSTAL@4" {
		t.Fatalf("item = %s", normalized.Item.ID)
	}
	if normalized.Location.Name != "Lymhurst" || normalized.Quality.Name != "Bueno" {
		t.Fatalf("dimensions = %+v %+v", normalized.Location, normalized.Quality)
	}
	if normalized.Summary.SoldUnits != 3 || normalized.Summary.TotalSilver != 2_713_316 {
		t.Fatalf("summary = %+v", normalized.Summary)
	}
	if normalized.Summary.WeightedAverageUnitPrice != 904438.6666666666 {
		t.Fatalf("weighted average = %v", normalized.Summary.WeightedAverageUnitPrice)
	}
	if got := normalized.History[0].Timestamp.Format(time.RFC3339); got != "2026-06-21T00:00:00Z" {
		t.Fatalf("timestamp = %s", got)
	}
}

func TestNormalizeLegacyFixtureKeepsAlreadyNormalizedSilver(t *testing.T) {
	service, _ := NewService(testCatalog(t), &memoryStore{})
	capture := domain.CapturedHistory{
		SchemaVersion: 1, Source: "test", Server: "west", CapturedAt: time.Now().UTC(),
		Payload: domain.MarketHistoriesUpload{
			AlbionID: 6826, LocationID: "1002", QualityLevel: 2, Timescale: domain.TimescaleHours,
			Histories: []*domain.MarketHistory{{ItemAmount: 1, SilverAmount: 1_250_000, Timestamp: 1_750_612_800}},
		},
	}
	normalized, err := service.NormalizeHistory(capture)
	if err != nil {
		t.Fatal(err)
	}
	if normalized.History[0].TotalSilver != 1_250_000 {
		t.Fatalf("silver = %d", normalized.History[0].TotalSilver)
	}
}

func TestNormalizeAndDeduplicateOrders(t *testing.T) {
	store := &memoryStore{}
	service, _ := NewService(testCatalog(t), store)
	upload := domain.MarketOrdersUpload{Orders: []*domain.MarketOrder{{
		ID: 10, ItemTypeID: "T4_MAIN_CURSEDSTAFF_CRYSTAL@4", ItemGroupTypeID: "T4_MAIN_CURSEDSTAFF_CRYSTAL",
		LocationID: "1002", QualityLevel: 2, EnchantmentLevel: 4, UnitPriceSilver: 9_099_970_000,
		Amount: 1, AuctionType: "offer", Expires: "2026-07-22T18:01:45.98371",
	}}}
	capturedAt := time.Date(2026, 6, 22, 18, 0, 0, 0, time.UTC)
	first, err := service.CaptureOrders(context.Background(), "test", "west", capturedAt, upload)
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.CaptureOrders(context.Background(), "test", "west", capturedAt.Add(time.Minute), upload)
	if err != nil {
		t.Fatal(err)
	}
	if first.Stored != 1 || second.Duplicates != 1 {
		t.Fatalf("first=%+v second=%+v", first, second)
	}
	if store.orders[0].UnitPrice != 909_997 || store.orders[0].Side != "sell" {
		t.Fatalf("order = %+v", store.orders[0])
	}
}

func TestNormalizeUsesCanonicalMarketplaceLocationForCityAlias(t *testing.T) {
	service, err := NewService(testCatalog(t), &memoryStore{})
	if err != nil {
		t.Fatal(err)
	}

	orders, err := service.NormalizeOrders(
		"aodp-http-ingest",
		"west",
		time.Date(2026, 6, 23, 21, 35, 30, 0, time.UTC),
		domain.MarketOrdersUpload{Orders: []*domain.MarketOrder{{
			ID:               99,
			ItemTypeID:       "T4_MAIN_CURSEDSTAFF_CRYSTAL@4",
			ItemGroupTypeID:  "T4_MAIN_CURSEDSTAFF_CRYSTAL",
			LocationID:       "1000",
			QualityLevel:     2,
			EnchantmentLevel: 4,
			UnitPriceSilver:  9_000_000,
			Amount:           1,
			AuctionType:      "offer",
			Expires:          "2026-07-23T21:35:30Z",
		}}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if got := orders[0].Location.ID; got != "1002" {
		t.Fatalf("canonical location = %q, want 1002", got)
	}
}

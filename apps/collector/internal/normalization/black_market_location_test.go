package normalization

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"albion-market-data/collector/internal/catalog"
	"albion-market-data/collector/internal/domain"
)

func TestNormalizeOrdersDistinguishesBlackMarketFromCaerleon(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	items := filepath.Join(directory, "items.txt")
	markets := filepath.Join(directory, "markets.json")
	if err := os.WriteFile(items, []byte("6826:T4_MAIN_CURSEDSTAFF_CRYSTAL@4:Adept's Rotcaller Staff\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	marketCatalog := `{"schemaVersion":1,"markets":[{"key":"caerleon","name":"Caerleon","type":"regular","cityLocationId":"3003","marketLocationId":"3005","enabled":true},{"key":"black_market","name":"Black Market","type":"black-market","cityLocationId":"3003","marketLocationId":null,"enabled":false}]}`
	if err := os.WriteFile(markets, []byte(marketCatalog), 0o600); err != nil {
		t.Fatal(err)
	}

	loaded, err := catalog.Load(items, markets)
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService(loaded, &memoryStore{})
	if err != nil {
		t.Fatal(err)
	}

	orders, err := service.NormalizeOrders(
		"albion-data-client",
		"west",
		time.Date(2026, 7, 15, 18, 55, 0, 0, time.UTC),
		domain.MarketOrdersUpload{Orders: []*domain.MarketOrder{
			{
				ID:               1,
				ItemTypeID:       "T4_MAIN_CURSEDSTAFF_CRYSTAL@4",
				ItemGroupTypeID:  "T4_MAIN_CURSEDSTAFF_CRYSTAL",
				LocationID:       "3003",
				QualityLevel:     1,
				EnchantmentLevel: 4,
				UnitPriceSilver:  10_000_000,
				Amount:           1,
				AuctionType:      "request",
				Expires:          "2026-08-15T18:55:00Z",
			},
			{
				ID:               2,
				ItemTypeID:       "T4_MAIN_CURSEDSTAFF_CRYSTAL@4",
				ItemGroupTypeID:  "T4_MAIN_CURSEDSTAFF_CRYSTAL",
				LocationID:       "3005",
				QualityLevel:     1,
				EnchantmentLevel: 4,
				UnitPriceSilver:  9_000_000,
				Amount:           1,
				AuctionType:      "offer",
				Expires:          "2026-08-15T18:55:00Z",
			},
		}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(orders) != 2 {
		t.Fatalf("orders = %d, want 2", len(orders))
	}
	if location := orders[0].Location; location.ID != "3003" || location.Name != "Black Market" || location.MarketKey != "black_market" {
		t.Fatalf("Black Market location = %+v", location)
	}
	if location := orders[1].Location; location.ID != "3005" || location.Name != "Caerleon" || location.MarketKey != "caerleon" {
		t.Fatalf("Caerleon location = %+v", location)
	}
}

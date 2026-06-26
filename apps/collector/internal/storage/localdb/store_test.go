package localdb

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"albion-market-data/collector/internal/domain"
	"albion-market-data/collector/internal/storage/queryjsonl"
)

func TestStorePersistsAndReloadsSnapshots(t *testing.T) {
	path := filepath.Join(t.TempDir(), "market-state.json")
	store, err := New(path)
	if err != nil {
		t.Fatal(err)
	}

	history := domain.NormalizedHistory{
		SchemaVersion: 1,
		Kind:          "market-history",
		Source:        "test",
		Server:        "west",
		Item:          domain.ItemDimension{AlbionID: 6826, ID: "T4_MAIN_CURSEDSTAFF_CRYSTAL@4"},
		Location:      domain.LocationDimension{ID: "5003", Name: "Brecilien"},
		Quality:       domain.QualityDimension{ID: 4, Name: "Excelente"},
		Period:        "4-weeks",
		CapturedAt:    time.Date(2026, 6, 22, 20, 0, 0, 0, time.UTC),
		Summary:       domain.NormalizedHistorySummary{SoldUnits: 1, ActiveBuckets: 1, TotalSilver: 1000, WeightedAverageUnitPrice: 1000},
		History:       []domain.NormalizedHistoryPoint{{Timestamp: time.Date(2026, 6, 21, 0, 0, 0, 0, time.UTC), ItemCount: 1, TotalSilver: 1000, AverageUnitPrice: 1000}},
		DedupeKey:     "history-key",
	}
	if stored, err := store.AppendHistory(context.Background(), history); err != nil || !stored {
		t.Fatalf("AppendHistory stored=%t err=%v", stored, err)
	}

	order := domain.NormalizedMarketOrder{
		SchemaVersion: 1,
		Kind:          "market-order",
		Source:        "test",
		Server:        "west",
		CapturedAt:    time.Date(2026, 6, 22, 20, 1, 0, 0, time.UTC),
		OrderID:       123,
		Item:          domain.ItemDimension{ID: "T4_MAIN_CURSEDSTAFF_CRYSTAL@4"},
		Location:      domain.LocationDimension{ID: "5003", Name: "Brecilien"},
		Quality:       domain.QualityDimension{ID: 4, Name: "Excelente"},
		AuctionType:   "offer",
		Side:          "sell",
		UnitPrice:     999,
		Amount:        1,
		ExpiresAt:     time.Date(2026, 7, 22, 20, 0, 0, 0, time.UTC),
		DedupeKey:     "order-key",
	}
	if written, duplicates, err := store.AppendOrders(context.Background(), []domain.NormalizedMarketOrder{order}); err != nil || written != 1 || duplicates != 0 {
		t.Fatalf("AppendOrders written=%d duplicates=%d err=%v", written, duplicates, err)
	}

	reloaded, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	histories, err := reloaded.ListHistories(context.Background(), queryjsonl.HistoryFilter{LocationName: "brecilien", Limit: 10})
	if err != nil || len(histories) != 1 {
		t.Fatalf("histories=%d err=%v", len(histories), err)
	}
	orders, err := reloaded.ListOrders(context.Background(), queryjsonl.OrderFilter{ItemID: order.Item.ID, Limit: 10})
	if err != nil || len(orders) != 1 || orders[0].UnitPrice != 999 {
		t.Fatalf("orders=%+v err=%v", orders, err)
	}
	stats := reloaded.Stats()
	if stats.HistorySnapshots != 1 || stats.OrderSnapshots != 1 || stats.Storage != "embedded-local-db" {
		t.Fatalf("stats=%+v", stats)
	}
}

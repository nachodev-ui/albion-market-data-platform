package normalizedjsonl

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"albion-market-data/collector/internal/domain"
)

func TestStoreDeduplicatesAcrossRestarts(t *testing.T) {
	directory := t.TempDir()
	store, err := NewStore(directory)
	if err != nil {
		t.Fatal(err)
	}
	history := domain.NormalizedHistory{
		SchemaVersion: 1, Kind: "market-history", Source: "test", Server: "west",
		Item: domain.ItemDimension{ID: "T4_TEST"}, Location: domain.LocationDimension{ID: "1301"},
		Quality: domain.QualityDimension{ID: 2, Name: "Bueno"}, Period: "7-days",
		CapturedAt: time.Date(2026, 6, 22, 0, 0, 0, 0, time.UTC),
		Summary:    domain.NormalizedHistorySummary{SoldUnits: 1, ActiveBuckets: 1, TotalSilver: 10, WeightedAverageUnitPrice: 10},
		History:    []domain.NormalizedHistoryPoint{{Timestamp: time.Date(2026, 6, 21, 0, 0, 0, 0, time.UTC), ItemCount: 1, TotalSilver: 10, AverageUnitPrice: 10}},
		DedupeKey:  "same-history",
	}
	stored, err := store.AppendHistory(context.Background(), history)
	if err != nil || !stored {
		t.Fatalf("first append stored=%v err=%v", stored, err)
	}
	store, err = NewStore(directory)
	if err != nil {
		t.Fatal(err)
	}
	stored, err = store.AppendHistory(context.Background(), history)
	if err != nil || stored {
		t.Fatalf("duplicate append stored=%v err=%v", stored, err)
	}
	content, err := os.ReadFile(filepath.Join(directory, "market-history-2026-06-22.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	lines := 0
	for _, char := range content {
		if char == '\n' {
			lines++
		}
	}
	if lines != 1 {
		t.Fatalf("lines=%d", lines)
	}
}

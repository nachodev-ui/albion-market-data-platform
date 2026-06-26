package jsonl

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"albion-market-data/collector/internal/domain"
)

func TestStoreAppend(t *testing.T) {
	directory := t.TempDir()
	store, err := NewStore(directory)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}

	capture := domain.CapturedHistory{
		SchemaVersion: 1,
		Source:        "test",
		Server:        "west",
		ItemID:        "T4_MAIN_CURSEDSTAFF_CRYSTAL@4",
		CapturedAt:    time.Date(2026, 6, 22, 18, 4, 17, 0, time.UTC),
		Payload: domain.MarketHistoriesUpload{
			AlbionID:     6826,
			LocationID:   "1301",
			QualityLevel: 2,
			Timescale:    domain.TimescaleHours,
			Histories: []*domain.MarketHistory{
				{ItemAmount: 1, SilverAmount: 1_250_000, Timestamp: 1_750_612_800},
			},
		},
	}

	if err := store.Append(context.Background(), capture); err != nil {
		t.Fatalf("append capture: %v", err)
	}

	path := filepath.Join(directory, "market-history-2026-06-22.jsonl")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read persisted file: %v", err)
	}
	if !strings.Contains(string(content), "T4_MAIN_CURSEDSTAFF_CRYSTAL@4") {
		t.Fatalf("persisted record does not contain item id: %s", content)
	}
}

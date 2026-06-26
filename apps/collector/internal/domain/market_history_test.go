package domain

import (
	"testing"
	"time"
)

func validCapture() CapturedHistory {
	return CapturedHistory{
		SchemaVersion: 1,
		Source:        "aodp-compatible-client",
		Server:        "west",
		ItemID:        "T4_MAIN_CURSEDSTAFF_CRYSTAL@4",
		CapturedAt:    time.Date(2026, 6, 22, 18, 4, 17, 0, time.UTC),
		Payload: MarketHistoriesUpload{
			AlbionID:     6826,
			LocationID:   "1301",
			QualityLevel: 2,
			Timescale:    TimescaleHours,
			Histories: []*MarketHistory{
				{ItemAmount: 1, SilverAmount: 1_250_000, Timestamp: 1_750_612_800},
			},
		},
	}
}

func TestCapturedHistoryValidate(t *testing.T) {
	capture := validCapture()
	if err := capture.Validate(); err != nil {
		t.Fatalf("expected valid capture, got %v", err)
	}
}

func TestCapturedHistoryRejectsInvalidQuality(t *testing.T) {
	capture := validCapture()
	capture.Payload.QualityLevel = 6

	if err := capture.Validate(); err == nil {
		t.Fatal("expected invalid quality to fail validation")
	}
}

func TestCapturedHistorySummary(t *testing.T) {
	capture := validCapture()
	capture.Payload.Histories = append(capture.Payload.Histories,
		&MarketHistory{ItemAmount: 2, SilverAmount: 2_400_000, Timestamp: 1_750_616_400},
	)

	summary := capture.Summary()
	if summary.TotalItems != 3 {
		t.Fatalf("expected 3 items, got %d", summary.TotalItems)
	}
	if summary.WeightedAveragePrice != 1_216_666 {
		t.Fatalf("unexpected weighted average: %d", summary.WeightedAveragePrice)
	}
}

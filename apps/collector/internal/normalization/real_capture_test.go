package normalization

import (
	"testing"
	"time"

	"albion-market-data/collector/internal/domain"
)

func TestRealSevenDayCaptureRegression(t *testing.T) {
	service, err := NewService(testCatalog(t), &memoryStore{})
	if err != nil {
		t.Fatal(err)
	}
	capture := domain.CapturedHistory{
		SchemaVersion: 1,
		Source:        "aodp-http-ingest",
		Server:        "west",
		CapturedAt:    time.Date(2026, 6, 22, 18, 51, 51, 775015400, time.UTC),
		Payload: domain.MarketHistoriesUpload{
			AlbionID:     6826,
			LocationID:   "1301",
			QualityLevel: 2,
			Timescale:    domain.TimescaleDays,
			Histories: []*domain.MarketHistory{
				{ItemAmount: 1, SilverAmount: 9_044_390_000, Timestamp: 639_175_968_000_000_000},
				{ItemAmount: 2, SilverAmount: 18_088_770_000, Timestamp: 639_175_752_000_000_000},
				{ItemAmount: 1, SilverAmount: 9_044_390_000, Timestamp: 639_174_240_000_000_000},
				{ItemAmount: 1, SilverAmount: 9_044_380_000, Timestamp: 639_174_024_000_000_000},
				{ItemAmount: 3, SilverAmount: 27_108_960_000, Timestamp: 639_173_376_000_000_000},
			},
		},
	}

	normalized, err := service.NormalizeHistory(capture)
	if err != nil {
		t.Fatal(err)
	}
	if normalized.Summary.SoldUnits != 8 {
		t.Fatalf("sold units = %d", normalized.Summary.SoldUnits)
	}
	if normalized.Summary.ActiveBuckets != 5 {
		t.Fatalf("active buckets = %d", normalized.Summary.ActiveBuckets)
	}
	if normalized.Summary.WeightedAverageUnitPrice != 904136.125 {
		t.Fatalf("weighted average = %.6f", normalized.Summary.WeightedAverageUnitPrice)
	}
	if got := normalized.History[0].Timestamp.Format(time.RFC3339); got != "2026-06-21T00:00:00Z" {
		t.Fatalf("latest timestamp = %s", got)
	}
}

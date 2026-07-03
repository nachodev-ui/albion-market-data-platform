package normalization

import (
	"testing"
	"time"

	"albion-market-data/collector/internal/domain"
)

func BenchmarkNormalizeHistory68Buckets(b *testing.B) {
	service, err := NewService(testCatalog(b), &memoryStore{})
	if err != nil {
		b.Fatal(err)
	}
	capture := benchmarkHistoryCapture(68)
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		history, err := service.NormalizeHistory(capture)
		if err != nil {
			b.Fatal(err)
		}
		if len(history.History) != 68 {
			b.Fatalf("history=%d", len(history.History))
		}
	}
}

func benchmarkHistoryCapture(bucketCount int) domain.CapturedHistory {
	capturedAt := time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC)
	points := make([]*domain.MarketHistory, bucketCount)
	for index := range points {
		points[index] = &domain.MarketHistory{
			ItemAmount:   int64(index + 1),
			SilverAmount: uint64(index+1) * 10_000_000,
			Timestamp:    uint64(capturedAt.Add(-time.Duration(index)*6*time.Hour).Unix()),
		}
	}
	return domain.CapturedHistory{
		SchemaVersion: 1,
		Source:        "benchmark",
		Server:        "west",
		CapturedAt:    capturedAt,
		Payload: domain.MarketHistoriesUpload{
			AlbionID:     6826,
			LocationID:   "1002",
			QualityLevel: 2,
			Timescale:    domain.TimescaleDays,
			Histories:    points,
		},
	}
}

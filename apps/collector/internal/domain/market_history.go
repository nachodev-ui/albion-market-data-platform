package domain

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// Timescale mirrors the three market-history views emitted by Albion:
// 0 = 24 hours, 1 = 7 days, 2 = 4 weeks.
type Timescale uint8

const (
	TimescaleHours Timescale = iota
	TimescaleDays
	TimescaleWeeks
)

func (t Timescale) String() string {
	switch t {
	case TimescaleHours:
		return "24-hours"
	case TimescaleDays:
		return "7-days"
	case TimescaleWeeks:
		return "4-weeks"
	default:
		return "unknown"
	}
}

// MarketHistory matches one history point produced by the AoData-compatible
// market-history decoder.
type MarketHistory struct {
	ItemAmount   int64  `json:"ItemAmount"`
	SilverAmount uint64 `json:"SilverAmount"`
	Timestamp    uint64 `json:"Timestamp"`
}

// MarketHistoriesUpload mirrors the decoded payload shape used by the AoData
// client. ItemID is intentionally not present in this payload; AlbionId must be
// resolved against an item catalog.
type MarketHistoriesUpload struct {
	AlbionID     int32            `json:"AlbionId"`
	LocationID   string           `json:"LocationId"`
	QualityLevel uint8            `json:"QualityLevel"`
	Timescale    Timescale        `json:"Timescale"`
	Histories    []*MarketHistory `json:"MarketHistories"`
}

// CapturedHistory adds the metadata our platform needs around the raw decoded
// payload. ItemID may initially be empty for live AoData uploads because the
// wire payload only includes AlbionId. A catalog resolver fills it later.
type CapturedHistory struct {
	SchemaVersion int                   `json:"schemaVersion"`
	Source        string                `json:"source"`
	Server        string                `json:"server"`
	ItemID        string                `json:"itemId"`
	CapturedAt    time.Time             `json:"capturedAt"`
	Payload       MarketHistoriesUpload `json:"payload"`
}

func (c CapturedHistory) Validate() error {
	if c.SchemaVersion != 1 {
		return fmt.Errorf("schemaVersion must be 1, got %d", c.SchemaVersion)
	}
	if strings.TrimSpace(c.Source) == "" {
		return errors.New("source is required")
	}
	if c.Server != "west" && c.Server != "east" && c.Server != "europe" {
		return fmt.Errorf("unsupported server %q", c.Server)
	}
	if c.CapturedAt.IsZero() {
		return errors.New("capturedAt is required")
	}
	if c.Payload.AlbionID <= 0 {
		return errors.New("payload.AlbionId must be greater than zero")
	}
	if strings.TrimSpace(c.Payload.LocationID) == "" {
		return errors.New("payload.LocationId is required")
	}
	if c.Payload.QualityLevel < 1 || c.Payload.QualityLevel > 5 {
		return fmt.Errorf("payload.QualityLevel must be between 1 and 5, got %d", c.Payload.QualityLevel)
	}
	if c.Payload.Timescale > TimescaleWeeks {
		return fmt.Errorf("payload.Timescale must be 0, 1 or 2, got %d", c.Payload.Timescale)
	}
	if len(c.Payload.Histories) == 0 {
		return errors.New("payload.MarketHistories must contain at least one point")
	}

	for index, point := range c.Payload.Histories {
		if point == nil {
			return fmt.Errorf("payload.MarketHistories[%d] cannot be null", index)
		}
		if point.ItemAmount < 0 {
			return fmt.Errorf("payload.MarketHistories[%d].ItemAmount cannot be negative", index)
		}
		if point.Timestamp == 0 {
			return fmt.Errorf("payload.MarketHistories[%d].Timestamp must be greater than zero", index)
		}
	}

	return nil
}

func (c CapturedHistory) Summary() HistorySummary {
	var totalItems int64
	var totalSilver uint64

	for _, point := range c.Payload.Histories {
		if point == nil {
			continue
		}
		totalItems += point.ItemAmount
		totalSilver += point.SilverAmount
	}

	var weightedAverage uint64
	if totalItems > 0 {
		weightedAverage = totalSilver / uint64(totalItems)
	}

	return HistorySummary{
		Points:               len(c.Payload.Histories),
		TotalItems:           totalItems,
		TotalSilver:          totalSilver,
		WeightedAveragePrice: weightedAverage,
	}
}

type HistorySummary struct {
	Points               int
	TotalItems           int64
	TotalSilver          uint64
	WeightedAveragePrice uint64
}

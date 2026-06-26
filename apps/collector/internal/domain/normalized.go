package domain

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

const NormalizedSchemaVersion = 1

type ItemDimension struct {
	AlbionID int32  `json:"albionId,omitempty"`
	ID       string `json:"id"`
	Name     string `json:"name,omitempty"`
}

type LocationDimension struct {
	ID        string `json:"id"`
	Name      string `json:"name,omitempty"`
	MarketKey string `json:"marketKey,omitempty"`
}

type MarketDefinition struct {
	Key              string  `json:"key"`
	Name             string  `json:"name"`
	Type             string  `json:"type"`
	CityLocationID   string  `json:"cityLocationId"`
	MarketLocationID *string `json:"marketLocationId"`
	Enabled          bool    `json:"enabled"`
}

type QualityDimension struct {
	ID   uint8  `json:"id"`
	Name string `json:"name"`
}

type NormalizedHistoryPoint struct {
	Timestamp        time.Time `json:"timestamp"`
	ItemCount        int64     `json:"itemCount"`
	TotalSilver      int64     `json:"totalSilver"`
	AverageUnitPrice float64   `json:"averageUnitPrice"`
}

type NormalizedHistorySummary struct {
	SoldUnits                int64   `json:"soldUnits"`
	ActiveBuckets            int     `json:"activeBuckets"`
	TotalSilver              int64   `json:"totalSilver"`
	WeightedAverageUnitPrice float64 `json:"weightedAverageUnitPrice"`
}

type NormalizedHistory struct {
	SchemaVersion int                      `json:"schemaVersion"`
	Kind          string                   `json:"kind"`
	Source        string                   `json:"source"`
	Server        string                   `json:"server"`
	Item          ItemDimension            `json:"item"`
	Location      LocationDimension        `json:"location"`
	Quality       QualityDimension         `json:"quality"`
	Period        string                   `json:"period"`
	CapturedAt    time.Time                `json:"capturedAt"`
	Summary       NormalizedHistorySummary `json:"summary"`
	History       []NormalizedHistoryPoint `json:"history"`
	DedupeKey     string                   `json:"dedupeKey"`
}

func (h NormalizedHistory) Validate() error {
	if h.SchemaVersion != NormalizedSchemaVersion {
		return fmt.Errorf("schemaVersion must be %d", NormalizedSchemaVersion)
	}
	if h.Kind != "market-history" {
		return fmt.Errorf("kind must be market-history, got %q", h.Kind)
	}
	if strings.TrimSpace(h.Item.ID) == "" {
		return errors.New("item.id is required")
	}
	if strings.TrimSpace(h.Location.ID) == "" {
		return errors.New("location.id is required")
	}
	if h.Quality.ID < 1 || h.Quality.ID > 5 {
		return fmt.Errorf("quality.id must be between 1 and 5, got %d", h.Quality.ID)
	}
	if h.CapturedAt.IsZero() {
		return errors.New("capturedAt is required")
	}
	if len(h.History) == 0 {
		return errors.New("history must contain at least one point")
	}
	if strings.TrimSpace(h.DedupeKey) == "" {
		return errors.New("dedupeKey is required")
	}
	return nil
}

type MarketOrder struct {
	ID               int64  `json:"Id"`
	ItemTypeID       string `json:"ItemTypeId"`
	ItemGroupTypeID  string `json:"ItemGroupTypeId"`
	LocationID       string `json:"LocationId"`
	QualityLevel     uint8  `json:"QualityLevel"`
	EnchantmentLevel uint8  `json:"EnchantmentLevel"`
	UnitPriceSilver  int64  `json:"UnitPriceSilver"`
	Amount           int64  `json:"Amount"`
	AuctionType      string `json:"AuctionType"`
	Expires          string `json:"Expires"`
}

type MarketOrdersUpload struct {
	Orders []*MarketOrder `json:"Orders"`
}

type NormalizedMarketOrder struct {
	SchemaVersion    int               `json:"schemaVersion"`
	Kind             string            `json:"kind"`
	Source           string            `json:"source"`
	Server           string            `json:"server"`
	CapturedAt       time.Time         `json:"capturedAt"`
	OrderID          int64             `json:"orderId"`
	Item             ItemDimension     `json:"item"`
	ItemGroupID      string            `json:"itemGroupId"`
	EnchantmentLevel uint8             `json:"enchantmentLevel"`
	Location         LocationDimension `json:"location"`
	Quality          QualityDimension  `json:"quality"`
	AuctionType      string            `json:"auctionType"`
	Side             string            `json:"side"`
	UnitPrice        int64             `json:"unitPrice"`
	Amount           int64             `json:"amount"`
	ExpiresAt        time.Time         `json:"expiresAt"`
	DedupeKey        string            `json:"dedupeKey"`
}

func (o NormalizedMarketOrder) Validate() error {
	if o.SchemaVersion != NormalizedSchemaVersion {
		return fmt.Errorf("schemaVersion must be %d", NormalizedSchemaVersion)
	}
	if o.Kind != "market-order" {
		return fmt.Errorf("kind must be market-order, got %q", o.Kind)
	}
	if o.OrderID <= 0 {
		return errors.New("orderId must be greater than zero")
	}
	if strings.TrimSpace(o.Item.ID) == "" {
		return errors.New("item.id is required")
	}
	if o.UnitPrice < 0 || o.Amount < 0 {
		return errors.New("unitPrice and amount cannot be negative")
	}
	if o.ExpiresAt.IsZero() {
		return errors.New("expiresAt is required")
	}
	if strings.TrimSpace(o.DedupeKey) == "" {
		return errors.New("dedupeKey is required")
	}
	return nil
}

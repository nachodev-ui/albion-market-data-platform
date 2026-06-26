package normalization

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"albion-market-data/collector/internal/catalog"
	"albion-market-data/collector/internal/domain"
)

const (
	dotNetUnixEpochTicks uint64 = 621_355_968_000_000_000
	ticksPerSecond       uint64 = 10_000_000
	silverWireScale      uint64 = 10_000
)

type Store interface {
	AppendHistory(ctx context.Context, history domain.NormalizedHistory) (bool, error)
	AppendOrders(ctx context.Context, orders []domain.NormalizedMarketOrder) (written int, duplicates int, err error)
}

type Service struct {
	catalog *catalog.Catalog
	store   Store
}

type OrdersResult struct {
	Received   int `json:"received"`
	Stored     int `json:"stored"`
	Duplicates int `json:"duplicates"`
}

func NewService(itemCatalog *catalog.Catalog, store Store) (*Service, error) {
	if itemCatalog == nil {
		return nil, fmt.Errorf("catalog is required")
	}
	if store == nil {
		return nil, fmt.Errorf("normalized store is required")
	}
	return &Service{catalog: itemCatalog, store: store}, nil
}

func (s *Service) CaptureHistory(ctx context.Context, capture domain.CapturedHistory) (domain.NormalizedHistory, bool, error) {
	normalized, err := s.NormalizeHistory(capture)
	if err != nil {
		return domain.NormalizedHistory{}, false, err
	}
	stored, err := s.store.AppendHistory(ctx, normalized)
	if err != nil {
		return domain.NormalizedHistory{}, false, fmt.Errorf("persist normalized history: %w", err)
	}
	return normalized, stored, nil
}

func (s *Service) NormalizeHistory(capture domain.CapturedHistory) (domain.NormalizedHistory, error) {
	if err := capture.Validate(); err != nil {
		return domain.NormalizedHistory{}, fmt.Errorf("validate captured history: %w", err)
	}

	item, ok := s.catalog.ItemByAlbionID(capture.Payload.AlbionID)
	if !ok {
		return domain.NormalizedHistory{}, fmt.Errorf("AlbionId %d is not present in catalog/items.txt", capture.Payload.AlbionID)
	}
	if capture.ItemID != "" && capture.ItemID != item.ID {
		return domain.NormalizedHistory{}, fmt.Errorf("itemId %q does not match AlbionId %d (%s)", capture.ItemID, capture.Payload.AlbionID, item.ID)
	}
	quality, err := catalog.Quality(capture.Payload.QualityLevel)
	if err != nil {
		return domain.NormalizedHistory{}, err
	}

	points := make([]domain.NormalizedHistoryPoint, 0, len(capture.Payload.Histories))
	var soldUnits int64
	var totalSilver int64
	activeBuckets := 0
	for index, point := range capture.Payload.Histories {
		if point == nil {
			return domain.NormalizedHistory{}, fmt.Errorf("history point %d cannot be null", index)
		}
		timestamp, officialWireEncoding, err := normalizeTimestamp(point.Timestamp)
		if err != nil {
			return domain.NormalizedHistory{}, fmt.Errorf("normalize history point %d timestamp: %w", index, err)
		}
		silver, err := normalizeSilver(point.SilverAmount, officialWireEncoding)
		if err != nil {
			return domain.NormalizedHistory{}, fmt.Errorf("normalize history point %d silver: %w", index, err)
		}
		average := float64(0)
		if point.ItemAmount > 0 {
			average = float64(silver) / float64(point.ItemAmount)
			activeBuckets++
		}
		points = append(points, domain.NormalizedHistoryPoint{
			Timestamp:        timestamp,
			ItemCount:        point.ItemAmount,
			TotalSilver:      silver,
			AverageUnitPrice: average,
		})
		soldUnits += point.ItemAmount
		if silver > math.MaxInt64-totalSilver {
			return domain.NormalizedHistory{}, fmt.Errorf("total silver overflows int64")
		}
		totalSilver += silver
	}

	sort.Slice(points, func(i, j int) bool {
		return points[i].Timestamp.After(points[j].Timestamp)
	})
	weightedAverage := float64(0)
	if soldUnits > 0 {
		weightedAverage = float64(totalSilver) / float64(soldUnits)
	}

	normalized := domain.NormalizedHistory{
		SchemaVersion: domain.NormalizedSchemaVersion,
		Kind:          "market-history",
		Source:        capture.Source,
		Server:        capture.Server,
		Item:          item,
		Location:      s.catalog.CanonicalMarketLocation(capture.Payload.LocationID),
		Quality:       quality,
		Period:        capture.Payload.Timescale.String(),
		CapturedAt:    capture.CapturedAt.UTC(),
		Summary: domain.NormalizedHistorySummary{
			SoldUnits:                soldUnits,
			ActiveBuckets:            activeBuckets,
			TotalSilver:              totalSilver,
			WeightedAverageUnitPrice: weightedAverage,
		},
		History: points,
	}
	normalized.DedupeKey, err = historyDedupeKey(normalized)
	if err != nil {
		return domain.NormalizedHistory{}, err
	}
	if err := normalized.Validate(); err != nil {
		return domain.NormalizedHistory{}, fmt.Errorf("validate normalized history: %w", err)
	}
	return normalized, nil
}

func (s *Service) CaptureOrders(ctx context.Context, source, server string, capturedAt time.Time, upload domain.MarketOrdersUpload) (OrdersResult, error) {
	_, result, err := s.CaptureOrdersDetailed(ctx, source, server, capturedAt, upload)
	if err != nil {
		return OrdersResult{}, err
	}
	return result, nil
}

func (s *Service) CaptureOrdersDetailed(ctx context.Context, source, server string, capturedAt time.Time, upload domain.MarketOrdersUpload) ([]domain.NormalizedMarketOrder, OrdersResult, error) {
	orders, err := s.NormalizeOrders(source, server, capturedAt, upload)
	if err != nil {
		return nil, OrdersResult{}, err
	}
	written, duplicates, err := s.store.AppendOrders(ctx, orders)
	if err != nil {
		return nil, OrdersResult{}, fmt.Errorf("persist normalized orders: %w", err)
	}
	result := OrdersResult{Received: len(upload.Orders), Stored: written, Duplicates: duplicates}
	return orders, result, nil
}

func (s *Service) NormalizeOrders(source, server string, capturedAt time.Time, upload domain.MarketOrdersUpload) ([]domain.NormalizedMarketOrder, error) {
	if server != "west" && server != "east" && server != "europe" {
		return nil, fmt.Errorf("unsupported server %q", server)
	}
	if capturedAt.IsZero() {
		return nil, fmt.Errorf("capturedAt is required")
	}

	result := make([]domain.NormalizedMarketOrder, 0, len(upload.Orders))
	for index, raw := range upload.Orders {
		if raw == nil {
			return nil, fmt.Errorf("orders[%d] cannot be null", index)
		}
		if raw.ID <= 0 || strings.TrimSpace(raw.ItemTypeID) == "" || strings.TrimSpace(raw.LocationID) == "" {
			return nil, fmt.Errorf("orders[%d] is missing id, item or location", index)
		}
		if raw.UnitPriceSilver < 0 || raw.Amount < 0 {
			return nil, fmt.Errorf("orders[%d] price and amount cannot be negative", index)
		}
		if raw.UnitPriceSilver%int64(silverWireScale) != 0 {
			return nil, fmt.Errorf("orders[%d].UnitPriceSilver=%d is not divisible by %d", index, raw.UnitPriceSilver, silverWireScale)
		}
		quality, err := catalog.Quality(raw.QualityLevel)
		if err != nil {
			return nil, fmt.Errorf("orders[%d]: %w", index, err)
		}
		expiresAt, err := parseAlbionTime(raw.Expires)
		if err != nil {
			return nil, fmt.Errorf("orders[%d].Expires: %w", index, err)
		}
		item, ok := s.catalog.ItemByID(raw.ItemTypeID)
		if !ok {
			item = domain.ItemDimension{ID: raw.ItemTypeID}
		}
		side := "unknown"
		switch raw.AuctionType {
		case "offer":
			side = "sell"
		case "request":
			side = "buy"
		}
		order := domain.NormalizedMarketOrder{
			SchemaVersion:    domain.NormalizedSchemaVersion,
			Kind:             "market-order",
			Source:           source,
			Server:           server,
			CapturedAt:       capturedAt.UTC(),
			OrderID:          raw.ID,
			Item:             item,
			ItemGroupID:      raw.ItemGroupTypeID,
			EnchantmentLevel: raw.EnchantmentLevel,
			Location:         s.catalog.CanonicalMarketLocation(raw.LocationID),
			Quality:          quality,
			AuctionType:      raw.AuctionType,
			Side:             side,
			UnitPrice:        raw.UnitPriceSilver / int64(silverWireScale),
			Amount:           raw.Amount,
			ExpiresAt:        expiresAt,
		}
		order.DedupeKey, err = orderDedupeKey(order)
		if err != nil {
			return nil, err
		}
		if err := order.Validate(); err != nil {
			return nil, fmt.Errorf("validate normalized order %d: %w", raw.ID, err)
		}
		result = append(result, order)
	}
	return result, nil
}

func normalizeTimestamp(value uint64) (time.Time, bool, error) {
	if value >= dotNetUnixEpochTicks {
		delta := value - dotNetUnixEpochTicks
		seconds := delta / ticksPerSecond
		nanos := (delta % ticksPerSecond) * 100
		if seconds > math.MaxInt64 {
			return time.Time{}, true, fmt.Errorf(".NET ticks exceed supported range")
		}
		return time.Unix(int64(seconds), int64(nanos)).UTC(), true, nil
	}
	// Compatibilidad exclusiva con las muestras del Hito 1/2, que usaban Unix
	// seconds y plata ya normalizada. Los paquetes reales usan ticks de .NET.
	if value <= 253_402_300_799 {
		return time.Unix(int64(value), 0).UTC(), false, nil
	}
	return time.Time{}, false, fmt.Errorf("unsupported timestamp value %d", value)
}

func normalizeSilver(value uint64, officialWireEncoding bool) (int64, error) {
	if officialWireEncoding {
		if value%silverWireScale != 0 {
			return 0, fmt.Errorf("wire value %d is not divisible by %d", value, silverWireScale)
		}
		value /= silverWireScale
	}
	if value > math.MaxInt64 {
		return 0, fmt.Errorf("silver value exceeds int64")
	}
	return int64(value), nil
}

func parseAlbionTime(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, fmt.Errorf("timestamp is empty")
	}
	if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return parsed.UTC(), nil
	}
	layouts := []string{
		"2006-01-02T15:04:05.999999999",
		"2006-01-02T15:04:05",
	}
	for _, layout := range layouts {
		if parsed, err := time.ParseInLocation(layout, value, time.UTC); err == nil {
			return parsed.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("unsupported timestamp %q", value)
}

func historyDedupeKey(history domain.NormalizedHistory) (string, error) {
	payload := struct {
		Server   string                          `json:"server"`
		ItemID   string                          `json:"itemId"`
		Location string                          `json:"location"`
		Quality  uint8                           `json:"quality"`
		Period   string                          `json:"period"`
		History  []domain.NormalizedHistoryPoint `json:"history"`
	}{history.Server, history.Item.ID, history.Location.ID, history.Quality.ID, history.Period, history.History}
	return hashJSON(payload)
}

func orderDedupeKey(order domain.NormalizedMarketOrder) (string, error) {
	payload := struct {
		Server      string    `json:"server"`
		OrderID     int64     `json:"orderId"`
		ItemID      string    `json:"itemId"`
		Location    string    `json:"location"`
		Quality     uint8     `json:"quality"`
		AuctionType string    `json:"auctionType"`
		UnitPrice   int64     `json:"unitPrice"`
		Amount      int64     `json:"amount"`
		ExpiresAt   time.Time `json:"expiresAt"`
	}{order.Server, order.OrderID, order.Item.ID, order.Location.ID, order.Quality.ID, order.AuctionType, order.UnitPrice, order.Amount, order.ExpiresAt}
	return hashJSON(payload)
}

func hashJSON(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("encode dedupe payload: %w", err)
	}
	hash := sha256.Sum256(encoded)
	return hex.EncodeToString(hash[:]), nil
}

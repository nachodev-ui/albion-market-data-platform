package httpingest

import (
	"fmt"
	"sort"
	"strconv"
	"time"

	"albion-market-data/collector/internal/domain"
	"albion-market-data/collector/internal/upstream"
)

type snapshotKey struct {
	locationID int16
	itemKey    string
	quality    int16
}

type snapshotState struct {
	observedAt     time.Time
	sellPriceMin   *int64
	sellPriceMinAt *time.Time
	buyPriceMax    *int64
	buyPriceMaxAt  *time.Time
}

func buildUpstreamPriceEntries(orders []domain.NormalizedMarketOrder) ([]upstream.PriceIngest, error) {
	groups := make(map[snapshotKey]*snapshotState)

	for _, order := range orders {
		if order.Amount <= 0 {
			continue
		}
		if !order.ExpiresAt.After(order.CapturedAt) {
			continue
		}

		locationID, err := parseLocationID(order.Location.ID)
		if err != nil {
			return nil, err
		}

		key := snapshotKey{
			locationID: locationID,
			itemKey:    order.Item.ID,
			quality:    int16(order.Quality.ID),
		}
		state := groups[key]
		if state == nil {
			state = &snapshotState{}
			groups[key] = state
		}
		capturedAt := order.CapturedAt.UTC()
		if capturedAt.After(state.observedAt) {
			state.observedAt = capturedAt
		}

		switch order.Side {
		case "sell":
			if state.sellPriceMin == nil || order.UnitPrice < *state.sellPriceMin {
				price := order.UnitPrice
				timestamp := capturedAt
				state.sellPriceMin = &price
				state.sellPriceMinAt = &timestamp
			}
		case "buy":
			if state.buyPriceMax == nil || order.UnitPrice > *state.buyPriceMax {
				price := order.UnitPrice
				timestamp := capturedAt
				state.buyPriceMax = &price
				state.buyPriceMaxAt = &timestamp
			}
		}
	}

	keys := make([]snapshotKey, 0, len(groups))
	for key, state := range groups {
		if state.sellPriceMin == nil && state.buyPriceMax == nil {
			continue
		}
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].locationID != keys[j].locationID {
			return keys[i].locationID < keys[j].locationID
		}
		if keys[i].itemKey != keys[j].itemKey {
			return keys[i].itemKey < keys[j].itemKey
		}
		return keys[i].quality < keys[j].quality
	})

	entries := make([]upstream.PriceIngest, 0, len(keys))
	for _, key := range keys {
		state := groups[key]
		entries = append(entries, upstream.PriceIngest{
			ObservedAt:     state.observedAt,
			LocationID:     key.locationID,
			ItemKey:        key.itemKey,
			Quality:        key.quality,
			SellPriceMin:   state.sellPriceMin,
			SellPriceMinAt: state.sellPriceMinAt,
			BuyPriceMax:    state.buyPriceMax,
			BuyPriceMaxAt:  state.buyPriceMaxAt,
		})
	}

	return entries, nil
}

func parseLocationID(value string) (int16, error) {
	parsed, err := strconv.ParseInt(value, 10, 16)
	if err != nil {
		return 0, fmt.Errorf("location %q is not a numeric market identifier", value)
	}
	if parsed <= 0 {
		return 0, fmt.Errorf("location %q must be greater than zero", value)
	}
	return int16(parsed), nil
}

func buildUpstreamHistoryEntry(history domain.NormalizedHistory) (upstream.HistoryIngest, error) {
	locationID, err := parseLocationID(history.Location.ID)
	if err != nil {
		return upstream.HistoryIngest{}, err
	}
	if len(history.History) == 0 {
		return upstream.HistoryIngest{}, fmt.Errorf("history for %s has no buckets", history.Item.ID)
	}

	buckets := make([]upstream.HistoryBucketIngest, 0, len(history.History))
	for index, point := range history.History {
		if point.Timestamp.IsZero() {
			return upstream.HistoryIngest{}, fmt.Errorf("history bucket %d has no timestamp", index)
		}
		if point.ItemCount < 0 {
			return upstream.HistoryIngest{}, fmt.Errorf("history bucket %d has a negative item count", index)
		}

		var averageUnitPrice *int64
		if point.ItemCount > 0 && point.TotalSilver > 0 {
			average := point.TotalSilver / point.ItemCount
			if average > 0 {
				averageUnitPrice = &average
			}
		}
		buckets = append(buckets, upstream.HistoryBucketIngest{
			Timestamp:        point.Timestamp.UTC(),
			ItemCount:        point.ItemCount,
			AverageUnitPrice: averageUnitPrice,
		})
	}

	return upstream.HistoryIngest{
		ObservedAt: history.CapturedAt.UTC(),
		LocationID: locationID,
		ItemKey:    history.Item.ID,
		Quality:    int16(history.Quality.ID),
		History:    buckets,
	}, nil
}

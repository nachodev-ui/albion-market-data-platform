package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"albion-market-data/collector/internal/catalog"
	"albion-market-data/collector/internal/domain"
	"albion-market-data/collector/internal/upstream"
)

const dotNetUnixEpochTicks uint64 = 621_355_968_000_000_000

func createCatalog(root string) (*catalog.Catalog, error) {
	itemsPath := filepath.Join(root, "items.txt")
	marketsPath := filepath.Join(root, "markets.json")
	var builder strings.Builder
	for index := 0; index < 10000; index++ {
		fmt.Fprintf(&builder, "%d: T4_ITEM_%d : Performance item %d\n", index+1, index, index)
	}
	builder.WriteString("20001: T4_BAG : Bag\n")
	if err := os.WriteFile(itemsPath, []byte(builder.String()), 0o600); err != nil {
		return nil, err
	}
	markets := `{"schemaVersion":1,"markets":[{"key":"caerleon","name":"Caerleon","type":"regular","cityLocationId":"3005","marketLocationId":"3005","enabled":true}]}`
	if err := os.WriteFile(marketsPath, []byte(markets), 0o600); err != nil {
		return nil, err
	}
	return catalog.Load(itemsPath, marketsPath)
}

func makeOrderUpload(count int) domain.MarketOrdersUpload {
	orders := make([]*domain.MarketOrder, count)
	for index := range orders {
		orders[index] = &domain.MarketOrder{ID: int64(index + 1), ItemTypeID: fmt.Sprintf("T4_ITEM_%d", index), ItemGroupTypeID: "T4_ITEM",
			LocationID: "3005", QualityLevel: uint8(index%5 + 1), UnitPriceSilver: int64(1000+index) * 10000,
			Amount: 1, AuctionType: "offer", Expires: "2026-08-02T12:00:00Z"}
	}
	return domain.MarketOrdersUpload{Orders: orders}
}

func makePriceEntries(count int) []upstream.PriceIngest {
	now := baselineTime()
	entries := make([]upstream.PriceIngest, count)
	for index := range entries {
		price := int64(1000 + index)
		entries[index] = upstream.PriceIngest{ObservedAt: now, LocationID: 3005, ItemKey: fmt.Sprintf("T4_ITEM_%d", index), Quality: int16(index%5 + 1), SellPriceMin: &price}
	}
	return entries
}

func makeHistoryCapture(buckets int) domain.CapturedHistory {
	capturedAt := baselineTime()
	points := make([]*domain.MarketHistory, buckets)
	for index := range points {
		pointTime := capturedAt.Add(-time.Duration(index) * 6 * time.Hour)
		points[index] = &domain.MarketHistory{ItemAmount: int64(index%9 + 1), SilverAmount: uint64(1000+index) * 10000,
			Timestamp: dotNetUnixEpochTicks + uint64(pointTime.Unix())*10_000_000}
	}
	return domain.CapturedHistory{SchemaVersion: 1, Source: "perfbaseline", Server: "west", CapturedAt: capturedAt,
		Payload: domain.MarketHistoriesUpload{AlbionID: 1, LocationID: "3005", QualityLevel: 1, Timescale: domain.TimescaleWeeks, Histories: points}}
}
func baselineTime() time.Time { return time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC) }

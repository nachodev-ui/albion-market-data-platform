// Performance harness for fictional Albion Online in-game market data; no financial trading or real-money transactions.
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"albion-market-data/collector/internal/domain"
	"albion-market-data/collector/internal/storage/localdb"
	"albion-market-data/collector/internal/storage/queryjsonl"
)

func addReadScenarios(target *report, root string, samples int, orders []domain.NormalizedMarketOrder, history domain.NormalizedHistory) error {
	directory, err := os.MkdirTemp(root, "read-fixture-")
	if err != nil {
		return err
	}
	db, err := localdb.New(filepath.Join(directory, "market-state.json"))
	if err != nil {
		return err
	}
	if _, _, err := db.AppendOrders(context.Background(), orders); err != nil {
		return err
	}
	for index := 0; index < 100; index++ {
		record := history
		record.CapturedAt = record.CapturedAt.Add(time.Duration(index) * time.Minute)
		record.DedupeKey = fmt.Sprintf("history-%03d", index)
		if _, err := db.AppendHistory(context.Background(), record); err != nil {
			return err
		}
	}
	if err := addMeasured(target, "read_prices_100", samples, func() (sampleDetails, error) {
		rows, err := db.ListOrders(context.Background(), queryjsonl.OrderFilter{Server: "west", Limit: 100})
		return sampleDetails{Counters: map[string]int64{"rows": int64(len(rows))}}, err
	}); err != nil {
		return err
	}
	return addMeasured(target, "read_history_100", samples, func() (sampleDetails, error) {
		rows, err := db.ListHistories(context.Background(), queryjsonl.HistoryFilter{Server: "west", Limit: 100})
		return sampleDetails{Counters: map[string]int64{"rows": int64(len(rows))}}, err
	})
}

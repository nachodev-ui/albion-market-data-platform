// Performance harness for fictional Albion Online in-game market data; no financial trading or real-money transactions.
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"albion-market-data/collector/internal/domain"
	"albion-market-data/collector/internal/storage/normalizedjsonl"
)

func addNormalizedFileScenario(target *report, root string, samples, size int, normalized []domain.NormalizedMarketOrder) error {
	return addMeasured(target, fmt.Sprintf("append_normalized_jsonl_%d", size), samples, func() (sampleDetails, error) {
		directory, err := os.MkdirTemp(root, "jsonl-")
		if err != nil {
			return sampleDetails{}, err
		}
		defer os.RemoveAll(directory)
		store, err := normalizedjsonl.NewStore(directory)
		if err != nil {
			return sampleDetails{}, err
		}
		written, duplicates, err := store.AppendOrders(context.Background(), normalized)
		return sampleDetails{Artifacts: map[string]int64{"normalized_jsonl": fileSize(filepath.Join(directory, "market-orders-2026-07-03.jsonl"))}, Counters: map[string]int64{"written": int64(written), "duplicates": int64(duplicates)}}, err
	})
}

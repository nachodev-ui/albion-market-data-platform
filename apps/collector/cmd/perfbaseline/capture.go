// Measures one complete capture through the Albion Online game-data persistence pipeline.
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"albion-market-data/collector/internal/catalog"
	"albion-market-data/collector/internal/normalization"
	"albion-market-data/collector/internal/storage/localdb"
	"albion-market-data/collector/internal/storage/normalizedjsonl"
)

func addCaptureScenario(target *report, root string, samples, size int, itemCatalog *catalog.Catalog, upload capturedOrderBatch) error {
	return addMeasured(target, fmt.Sprintf("capture_orders_%d", size), samples, func() (sampleDetails, error) {
		directory, err := os.MkdirTemp(root, "capture-")
		if err != nil {
			return sampleDetails{}, err
		}
		defer os.RemoveAll(directory)
		fileStore, err := normalizedjsonl.NewStore(filepath.Join(directory, "normalized"))
		if err != nil {
			return sampleDetails{}, err
		}
		statePath := filepath.Join(directory, "database", "state.json")
		stateStore, err := localdb.New(statePath)
		if err != nil {
			return sampleDetails{}, err
		}
		service, err := normalization.NewService(itemCatalog, compositeStore{jsonl: fileStore, db: stateStore})
		if err != nil {
			return sampleDetails{}, err
		}
		_, result, err := service.CaptureOrdersDetailed(context.Background(), "perfbaseline", "west", baselineTime(), upload)
		return sampleDetails{Artifacts: map[string]int64{"normalized_jsonl": fileSize(filepath.Join(directory, "normalized", "market-orders-2026-07-03.jsonl")), "local_database": fileSize(statePath)}, Counters: map[string]int64{"received": int64(result.Received), "stored": int64(result.Stored)}}, err
	})
}

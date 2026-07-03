// Performance harness for fictional Albion Online in-game market data; no financial trading or real-money transactions.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"albion-market-data/collector/internal/upstream"
)

func addQueueScenarios(target *report, root string, samples, size int) error {
	entries := makePriceEntries(size)
	if err := addMeasured(target, fmt.Sprintf("enqueue_outbox_%d", size), samples, func() (sampleDetails, error) {
		directory, err := os.MkdirTemp(root, "outbox-")
		if err != nil {
			return sampleDetails{}, err
		}
		defer os.RemoveAll(directory)
		path := filepath.Join(directory, "state.json")
		box, err := upstream.NewOutbox(path)
		if err != nil {
			return sampleDetails{}, err
		}
		accepted, depth, err := box.EnqueuePrices("west", entries, size)
		return sampleDetails{Artifacts: map[string]int64{"outbox_state": fileSize(path)}, Counters: map[string]int64{"accepted": int64(accepted), "depth": int64(depth)}}, err
	}); err != nil {
		return err
	}
	return addOutboxRestartScenario(target, root, size, entries, samples)
}

package main

import (
	"albion-market-data/collector/internal/upstream"
	"fmt"
	"os"
	"path/filepath"
)

func addOutboxRestartScenario(target *report, root string, size int, prices []upstream.PriceIngest, samples int) error {
	directory, err := os.MkdirTemp(root, "restart-template-")
	if err != nil {
		return err
	}
	path := filepath.Join(directory, "state.json")
	box, err := upstream.NewOutbox(path)
	if err != nil {
		return err
	}
	accepted, _, err := box.EnqueuePrices("west", prices, size)
	if err != nil || accepted != size {
		return fmt.Errorf("prepare restart data: %v", err)
	}
	return addMeasured(target, fmt.Sprintf("restart_outbox_%d", size), samples, func() (sampleDetails, error) {
		reopened, err := upstream.NewOutbox(path)
		if err != nil {
			return sampleDetails{}, err
		}
		snapshot := reopened.Snapshot(upstream.PipelinePrices)
		return sampleDetails{Artifacts: map[string]int64{"outbox_state": fileSize(path)}, Counters: map[string]int64{"pending_entries": int64(snapshot.PendingEntries)}}, nil
	})
}

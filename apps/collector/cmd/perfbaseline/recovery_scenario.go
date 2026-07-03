package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"albion-market-data/collector/internal/upstream"
)

func addRecoveryScenario(target *report, root string, prices []upstream.PriceIngest, samples int) error {
	return addMeasured(target, "recover_after_upstream_error_1000", samples, func() (sampleDetails, error) {
		directory, err := os.MkdirTemp(root, "outbox-retry-")
		if err != nil {
			return sampleDetails{}, err
		}
		defer os.RemoveAll(directory)
		path := filepath.Join(directory, "state.json")
		box, err := upstream.NewOutbox(path)
		if err != nil {
			return sampleDetails{}, err
		}
		accepted, depth, err := box.EnqueuePrices("west", prices, len(prices))
		if err != nil || accepted != len(prices) {
			return sampleDetails{}, fmt.Errorf("enqueue retry fixture: %v", err)
		}
		batch, err := box.ClaimPriceBatch("west", 500)
		if err != nil || batch == nil {
			return sampleDetails{}, fmt.Errorf("claim failed batch: %v", err)
		}
		if err := box.Reschedule(batch.RequestID, 1, 503, "retry", time.Now().UTC()); err != nil {
			return sampleDetails{}, err
		}
		recovered, err := box.ClaimPriceBatch("west", 500)
		if err != nil || recovered == nil || recovered.RequestID != batch.RequestID {
			return sampleDetails{}, fmt.Errorf("reclaim retry batch: %v", err)
		}
		if err := box.Complete(recovered.RequestID); err != nil {
			return sampleDetails{}, err
		}
		second, err := box.ClaimPriceBatch("west", 500)
		if err != nil || second == nil {
			return sampleDetails{}, fmt.Errorf("claim remaining batch: %v", err)
		}
		if err := box.Complete(second.RequestID); err != nil {
			return sampleDetails{}, err
		}
		snapshot := box.Snapshot(upstream.PipelinePrices)
		return sampleDetails{Artifacts: map[string]int64{"outbox_state": fileSize(path)}, Counters: map[string]int64{
			"queue_high_watermark": int64(depth), "rescheduled_batches": int64(snapshot.RescheduledBatchesTotal),
			"completed_batches": int64(snapshot.CompletedBatchesTotal), "pending_entries": int64(snapshot.PendingEntries),
		}}, nil
	})
}

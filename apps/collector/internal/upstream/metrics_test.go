package upstream

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func TestMetricsConcurrentAccess(t *testing.T) {
	metrics := NewMetrics()
	metrics.SetRunning(true)

	const workers = 20
	const iterations = 100
	var wait sync.WaitGroup
	wait.Add(workers)
	for worker := 0; worker < workers; worker++ {
		go func() {
			defer wait.Done()
			for iteration := 0; iteration < iterations; iteration++ {
				metrics.RecordEnqueued(iteration % 50)
				metrics.BeginBatch()
				metrics.RecordAttempt(time.Millisecond, 202, nil)
				metrics.RecordSuccess(1, 2*time.Millisecond, 1, 202)
				_ = metrics.Snapshot(iteration%50, 50)
			}
		}()
	}
	wait.Wait()

	snapshot := metrics.Snapshot(0, 50)
	want := uint64(workers * iterations)
	if snapshot.Totals.EnqueuedEntries != want {
		t.Fatalf("enqueued = %d, want %d", snapshot.Totals.EnqueuedEntries, want)
	}
	if snapshot.Totals.BatchesSent != want || snapshot.Totals.EntriesSent != want {
		t.Fatalf("totals = %+v", snapshot.Totals)
	}
	if snapshot.InFlightBatches != 0 {
		t.Fatalf("in flight = %d", snapshot.InFlightBatches)
	}
}

func TestMetricsExposesLastErrorAndDegradedStatus(t *testing.T) {
	metrics := NewMetrics()
	metrics.SetRunning(true)
	metrics.BeginBatch()
	metrics.RecordAttempt(5*time.Millisecond, 503, errors.New("temporary upstream failure"))
	metrics.RecordFailure(500, 5*time.Millisecond, 503, errors.New("temporary upstream failure"))

	snapshot := metrics.Snapshot(9, 10)
	if snapshot.Status != "degraded" {
		t.Fatalf("status = %q", snapshot.Status)
	}
	if snapshot.LastError == "" || snapshot.LastErrorAt == nil {
		t.Fatalf("missing last error: %+v", snapshot)
	}
	if snapshot.Totals.FailedBatches != 1 || snapshot.Totals.EntriesDroppedAfterRetry != 500 {
		t.Fatalf("totals = %+v", snapshot.Totals)
	}
}

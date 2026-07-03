package upstream

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"albion-market-data/collector/internal/observability"
)

func TestForwarderBoundsOutboxWhileUpstreamIsUnavailable(t *testing.T) {
	outbox, err := NewOutbox(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	sender := &scriptedSender{responses: []scriptedResponse{{err: errors.New("network down")}}}
	forwarder, err := NewForwarderWithOutbox(
		sender,
		observability.NewLogger(&bytes.Buffer{}, "never"),
		"west",
		outbox,
		20,
		5,
		2*time.Millisecond,
		1,
		5*time.Millisecond,
		100,
		50*time.Millisecond,
	)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	forwarder.Start(ctx)

	accepted, dropped := forwarder.EnqueueBatch(benchmarkPrices(20))
	if accepted != 20 || dropped != 0 {
		t.Fatalf("initial accepted=%d dropped=%d", accepted, dropped)
	}
	waitFor(t, time.Second, func() bool {
		return forwarder.Snapshot().Totals.RescheduledBatches > 0
	})

	accepted, dropped = forwarder.EnqueueBatch(benchmarkPrices(5))
	if accepted != 0 || dropped != 5 {
		t.Fatalf("saturated accepted=%d dropped=%d", accepted, dropped)
	}
	snapshot := forwarder.Snapshot()
	if snapshot.Queue.Depth != 20 || snapshot.Queue.UtilizationPercent != 100 {
		t.Fatalf("queue=%+v", snapshot.Queue)
	}
	if snapshot.Status != "degraded" {
		t.Fatalf("status=%q", snapshot.Status)
	}
	if snapshot.Outbox.DeadLetterBatches != 0 {
		t.Fatalf("outbox=%+v", snapshot.Outbox)
	}

	cancel()
	forwarder.Stop()
}

func TestPerformanceBaselineOutboxRecovery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	outbox, err := NewOutbox(path)
	if err != nil {
		t.Fatal(err)
	}
	accepted, _, err := outbox.EnqueuePrices("west", benchmarkPrices(10000), 10000)
	if err != nil || accepted != 10000 {
		t.Fatalf("accepted=%d err=%v", accepted, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	emitMetric(t, "outbox_file_10000", float64(info.Size()), "bytes")

	restartStarted := time.Now()
	reopened, err := NewOutbox(path)
	if err != nil {
		t.Fatal(err)
	}
	emitMetric(t, "outbox_restart_10000", float64(time.Since(restartStarted))/float64(time.Millisecond), "ms")
	if reopened.Depth(PipelinePrices) != 10000 {
		t.Fatalf("depth=%d", reopened.Depth(PipelinePrices))
	}

	recoveryOutbox, err := NewOutbox(filepath.Join(t.TempDir(), "recovery.json"))
	if err != nil {
		t.Fatal(err)
	}
	sender := &scriptedSender{responses: []scriptedResponse{
		{err: errors.New("temporary failure")},
		{result: SendResult{StatusCode: 202, Response: IngestPricesResponse{Accepted: 1000}}},
	}}
	forwarder, err := NewForwarderWithOutbox(
		sender,
		observability.NewLogger(&bytes.Buffer{}, "never"),
		"west",
		recoveryOutbox,
		1000,
		1000,
		time.Millisecond,
		1,
		time.Millisecond,
		4,
		5*time.Millisecond,
	)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	forwarder.Start(ctx)
	started := time.Now()
	accepted, dropped := forwarder.EnqueueBatch(benchmarkPrices(1000))
	if accepted != 1000 || dropped != 0 {
		t.Fatalf("accepted=%d dropped=%d", accepted, dropped)
	}
	waitFor(t, 3*time.Second, func() bool {
		return forwarder.Snapshot().Totals.BatchesSent == 1
	})
	snapshot := forwarder.Snapshot()
	emitMetric(t, "upstream_recovery_1000", float64(time.Since(started))/float64(time.Millisecond), "ms")
	emitMetric(t, "queue_high_watermark_1000", float64(snapshot.Queue.HighWatermark), "entries")
	forwarder.Stop()
}

func emitMetric(t *testing.T, name string, value float64, unit string) {
	t.Helper()
	encoded, err := json.Marshal(map[string]any{"name": name, "value": value, "unit": unit})
	if err != nil {
		t.Fatal(err)
	}
	fmt.Printf("PERF_METRIC %s\n", encoded)
}

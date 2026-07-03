package upstream

import (
	"bytes"
	"context"
	"errors"
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

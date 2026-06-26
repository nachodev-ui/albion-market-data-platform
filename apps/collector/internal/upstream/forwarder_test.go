package upstream

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"albion-market-data/collector/internal/observability"
)

type scriptedSender struct {
	mu        sync.Mutex
	responses []scriptedResponse
	payloads  []IngestPricesRequest
	calls     int
}

type scriptedResponse struct {
	result SendResult
	err    error
}

func (s *scriptedSender) SendPrices(_ context.Context, payload IngestPricesRequest) (SendResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.payloads = append(s.payloads, payload)
	index := s.calls
	s.calls++
	if index >= len(s.responses) {
		index = len(s.responses) - 1
	}
	response := s.responses[index]
	return response.result, response.err
}

func (s *scriptedSender) CallCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func (s *scriptedSender) Payloads() []IngestPricesRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]IngestPricesRequest(nil), s.payloads...)
}

func TestForwarderRetriesAndRecordsRecovery(t *testing.T) {
	sender := &scriptedSender{responses: []scriptedResponse{
		{err: &SendError{StatusCode: 503, Duration: time.Millisecond, Message: "unavailable"}},
		{result: SendResult{StatusCode: 202, Duration: time.Millisecond, Response: IngestPricesResponse{Accepted: 1, CurrentRowsTouched: 1}}},
	}}
	var output bytes.Buffer
	logger := observability.NewLogger(&output, "never")
	forwarder, err := NewForwarder(sender, logger, "west", 10, 1, time.Hour, 3, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	forwarder.Start(ctx)
	if !forwarder.Enqueue(PriceIngest{ItemKey: "T4_TEST", LocationID: 4002, Quality: 1}) {
		t.Fatal("enqueue failed")
	}

	waitFor(t, time.Second, func() bool {
		return forwarder.Snapshot().Totals.BatchesSent == 1
	})
	forwarder.Stop()

	snapshot := forwarder.Snapshot()
	if sender.CallCount() != 2 {
		t.Fatalf("calls = %d", sender.CallCount())
	}
	if snapshot.Totals.Retries != 1 || snapshot.Totals.RecoveredBatches != 1 {
		t.Fatalf("totals = %+v", snapshot.Totals)
	}
	if snapshot.Totals.EntriesSent != 1 || snapshot.Totals.FailedBatches != 0 {
		t.Fatalf("totals = %+v", snapshot.Totals)
	}
	if !bytes.Contains(output.Bytes(), []byte("upstream.retry_scheduled")) || !bytes.Contains(output.Bytes(), []byte("upstream.batch_recovered")) {
		t.Fatalf("logs = %s", output.String())
	}
}

func TestForwarderRecordsQueueDrop(t *testing.T) {
	sender := &scriptedSender{responses: []scriptedResponse{{result: SendResult{StatusCode: 202}}}}
	var output bytes.Buffer
	forwarder, err := NewForwarder(sender, observability.NewLogger(&output, "never"), "west", 1, 1, time.Hour, 1, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if !forwarder.Enqueue(PriceIngest{ItemKey: "FIRST"}) {
		t.Fatal("first enqueue failed")
	}
	if forwarder.Enqueue(PriceIngest{ItemKey: "SECOND"}) {
		t.Fatal("second enqueue should have been dropped")
	}

	snapshot := forwarder.Snapshot()
	if snapshot.Queue.Depth != 1 || snapshot.Queue.Capacity != 1 {
		t.Fatalf("queue = %+v", snapshot.Queue)
	}
	if snapshot.Totals.QueueDroppedEntries != 1 {
		t.Fatalf("totals = %+v", snapshot.Totals)
	}
	if !bytes.Contains(output.Bytes(), []byte("upstream.queue_drop")) {
		t.Fatalf("logs = %s", output.String())
	}
	forwarder.Stop()
}

func TestForwarderConcurrentEnqueueAndSnapshot(t *testing.T) {
	sender := &scriptedSender{responses: []scriptedResponse{{result: SendResult{StatusCode: 202, Response: IngestPricesResponse{Accepted: 1}}}}}
	forwarder, err := NewForwarder(sender, observability.NewLogger(&bytes.Buffer{}, "never"), "west", 2048, 100, 5*time.Millisecond, 1, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	forwarder.Start(ctx)

	const workers = 10
	const perWorker = 100
	var wait sync.WaitGroup
	wait.Add(workers + 1)
	for worker := 0; worker < workers; worker++ {
		go func(worker int) {
			defer wait.Done()
			for index := 0; index < perWorker; index++ {
				forwarder.Enqueue(PriceIngest{ItemKey: "T4_TEST", LocationID: int16(worker + 1), Quality: 1})
			}
		}(worker)
	}
	go func() {
		defer wait.Done()
		for index := 0; index < 500; index++ {
			_ = forwarder.Snapshot()
		}
	}()
	wait.Wait()
	cancel()
	forwarder.Stop()

	snapshot := forwarder.Snapshot()
	if snapshot.Totals.EnqueuedEntries+snapshot.Totals.QueueDroppedEntries != workers*perWorker {
		t.Fatalf("totals = %+v", snapshot.Totals)
	}
}

func TestForwarderDropsBatchAfterRetries(t *testing.T) {
	sender := &scriptedSender{responses: []scriptedResponse{{err: errors.New("network down")}}}
	forwarder, err := NewForwarder(sender, observability.NewLogger(&bytes.Buffer{}, "never"), "west", 10, 1, time.Hour, 2, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	forwarder.Start(ctx)
	forwarder.Enqueue(PriceIngest{ItemKey: "T4_TEST"})
	waitFor(t, time.Second, func() bool { return forwarder.Snapshot().Totals.FailedBatches == 1 })
	forwarder.Stop()

	snapshot := forwarder.Snapshot()
	if snapshot.Totals.SendAttempts != 2 || snapshot.Totals.Retries != 1 || snapshot.Totals.EntriesDroppedAfterRetry != 1 {
		t.Fatalf("totals = %+v", snapshot.Totals)
	}
}

func waitFor(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition was not met before timeout")
}

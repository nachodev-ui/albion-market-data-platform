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

type scriptedHistorySender struct {
	mu        sync.Mutex
	responses []scriptedHistoryResponse
	payloads  []IngestHistoryRequest
	calls     int
}

type scriptedHistoryResponse struct {
	result HistorySendResult
	err    error
}

func (s *scriptedHistorySender) SendHistory(_ context.Context, payload IngestHistoryRequest) (HistorySendResult, error) {
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

func (s *scriptedHistorySender) Snapshot() (int, []IngestHistoryRequest) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls, append([]IngestHistoryRequest(nil), s.payloads...)
}

func historyEntry(item string, buckets int) HistoryIngest {
	points := make([]HistoryBucketIngest, buckets)
	for index := range points {
		points[index] = HistoryBucketIngest{
			Timestamp: time.Date(2026, 6, 25-index, 0, 0, 0, 0, time.UTC),
			ItemCount: int64(index + 1),
		}
	}
	return HistoryIngest{
		ObservedAt: time.Date(2026, 6, 26, 20, 0, 0, 0, time.UTC),
		LocationID: 4002,
		ItemKey:    item,
		Quality:    1,
		History:    points,
	}
}

func TestHistoryForwarderRetriesSameIdempotentBatchAndRecordsRecovery(t *testing.T) {
	sender := &scriptedHistorySender{responses: []scriptedHistoryResponse{
		{err: &SendError{StatusCode: 503, Duration: time.Millisecond, Message: "unavailable"}},
		{result: HistorySendResult{StatusCode: 202, Duration: time.Millisecond, Response: IngestHistoryResponse{
			AcceptedEntries: 1,
			AcceptedBuckets: 2,
		}}},
	}}
	var output bytes.Buffer
	forwarder, err := NewHistoryForwarder(
		sender,
		observability.NewLogger(&output, "never"),
		"west",
		10,
		1,
		100000,
		time.Hour,
		3,
		time.Millisecond,
	)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	forwarder.Start(ctx)
	if !forwarder.Enqueue(historyEntry("T4_TEST", 2)) {
		t.Fatal("enqueue failed")
	}

	waitFor(t, time.Second, func() bool {
		return forwarder.Snapshot().Totals.BatchesSent == 1
	})
	forwarder.Stop()

	calls, payloads := sender.Snapshot()
	if calls != 2 || len(payloads) != 2 {
		t.Fatalf("calls=%d payloads=%d", calls, len(payloads))
	}
	if payloads[0].RequestID == "" || payloads[0].RequestID != payloads[1].RequestID {
		t.Fatalf("request IDs changed across retry: %q %q", payloads[0].RequestID, payloads[1].RequestID)
	}
	snapshot := forwarder.Snapshot()
	if snapshot.Totals.Retries != 1 || snapshot.Totals.RecoveredBatches != 1 {
		t.Fatalf("totals = %+v", snapshot.Totals)
	}
	if snapshot.Totals.EntriesSent != 1 || snapshot.Totals.BucketsSent != 2 || snapshot.Totals.FailedBatches != 0 {
		t.Fatalf("totals = %+v", snapshot.Totals)
	}
	if !bytes.Contains(output.Bytes(), []byte("upstream.history_retry_scheduled")) || !bytes.Contains(output.Bytes(), []byte("upstream.history_batch_recovered")) {
		t.Fatalf("logs = %s", output.String())
	}
}

func TestHistoryForwarderSplitsBatchesByBucketBudget(t *testing.T) {
	sender := &scriptedHistorySender{responses: []scriptedHistoryResponse{{result: HistorySendResult{StatusCode: 202}}}}
	forwarder, err := NewHistoryForwarder(
		sender,
		observability.NewLogger(&bytes.Buffer{}, "never"),
		"west",
		10,
		10,
		3,
		time.Hour,
		1,
		time.Millisecond,
	)
	if err != nil {
		t.Fatal(err)
	}
	forwarder.Start(context.Background())
	forwarder.Enqueue(historyEntry("T4_ONE", 2))
	forwarder.Enqueue(historyEntry("T4_TWO", 2))
	forwarder.Stop()

	calls, payloads := sender.Snapshot()
	if calls != 2 || len(payloads) != 2 {
		t.Fatalf("calls=%d payloads=%+v", calls, payloads)
	}
	for index, payload := range payloads {
		if len(payload.Entries) != 1 || historyBucketCount(payload.Entries) != 2 {
			t.Fatalf("payload[%d] = %+v", index, payload)
		}
	}
}

func TestHistoryForwarderRecordsQueueAndBucketDrops(t *testing.T) {
	sender := &scriptedHistorySender{responses: []scriptedHistoryResponse{{result: HistorySendResult{StatusCode: 202}}}}
	var output bytes.Buffer
	forwarder, err := NewHistoryForwarder(
		sender,
		observability.NewLogger(&output, "never"),
		"west",
		1,
		1,
		100000,
		time.Hour,
		1,
		time.Millisecond,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !forwarder.Enqueue(historyEntry("T4_FIRST", 2)) {
		t.Fatal("first enqueue failed")
	}
	if forwarder.Enqueue(historyEntry("T4_SECOND", 3)) {
		t.Fatal("second enqueue should have been dropped")
	}

	snapshot := forwarder.Snapshot()
	if snapshot.Totals.QueueDroppedEntries != 1 || snapshot.Totals.QueueDroppedBuckets != 3 {
		t.Fatalf("totals = %+v", snapshot.Totals)
	}
	if !bytes.Contains(output.Bytes(), []byte("upstream.history_queue_drop")) {
		t.Fatalf("logs = %s", output.String())
	}
	forwarder.Stop()
}

func TestHistoryForwarderDropsBatchAfterRetries(t *testing.T) {
	sender := &scriptedHistorySender{responses: []scriptedHistoryResponse{{err: errors.New("network down")}}}
	forwarder, err := NewHistoryForwarder(
		sender,
		observability.NewLogger(&bytes.Buffer{}, "never"),
		"west",
		10,
		1,
		100000,
		time.Hour,
		2,
		time.Millisecond,
	)
	if err != nil {
		t.Fatal(err)
	}
	forwarder.Start(context.Background())
	forwarder.Enqueue(historyEntry("T4_TEST", 4))
	waitFor(t, time.Second, func() bool { return forwarder.Snapshot().Totals.FailedBatches == 1 })
	forwarder.Stop()

	snapshot := forwarder.Snapshot()
	if snapshot.Totals.SendAttempts != 2 || snapshot.Totals.Retries != 1 || snapshot.Totals.EntriesDroppedAfterRetry != 1 || snapshot.Totals.BucketsDroppedAfterRetry != 4 {
		t.Fatalf("totals = %+v", snapshot.Totals)
	}
}

func TestNewRequestIDIsUUID(t *testing.T) {
	value := newRequestID()
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		t.Fatalf("request ID = %q", value)
	}
}

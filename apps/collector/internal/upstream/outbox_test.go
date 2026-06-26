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

func TestOutboxRecoversProcessingBatchAfterRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "outbox", "state.json")
	outbox, err := NewOutbox(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := outbox.EnqueuePrice("west", PriceIngest{ItemKey: "T4_TEST", LocationID: 4002, Quality: 1}, 10); err != nil {
		t.Fatal(err)
	}
	claimed, err := outbox.ClaimPriceBatch("west", 10)
	if err != nil {
		t.Fatal(err)
	}
	if claimed == nil || claimed.RequestID == "" {
		t.Fatalf("claimed = %+v", claimed)
	}

	reopened, err := NewOutbox(path)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := reopened.Snapshot(PipelinePrices)
	if snapshot.RetryingBatches != 1 || snapshot.RecoveredBatchesTotal != 1 || snapshot.PendingEntries != 0 {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	reclaimed, err := reopened.ClaimPriceBatch("west", 10)
	if err != nil {
		t.Fatal(err)
	}
	if reclaimed == nil || reclaimed.RequestID != claimed.RequestID {
		t.Fatalf("request id changed: first=%+v second=%+v", claimed, reclaimed)
	}
}

func TestOutboxIsSharedAcrossPriceAndHistoryPipelines(t *testing.T) {
	outbox, err := NewOutbox(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := outbox.EnqueuePrice("west", PriceIngest{ItemKey: "T4_PRICE", LocationID: 4002, Quality: 1}, 10); err != nil {
		t.Fatal(err)
	}
	if _, err := outbox.EnqueueHistory("west", historyEntry("T4_HISTORY", 2), 10); err != nil {
		t.Fatal(err)
	}
	if got := outbox.Depth(PipelinePrices); got != 1 {
		t.Fatalf("price depth = %d", got)
	}
	if got := outbox.Depth(PipelineHistory); got != 1 {
		t.Fatalf("history depth = %d", got)
	}
	if _, err := outbox.ClaimPriceBatch("west", 10); err != nil {
		t.Fatal(err)
	}
	if got := outbox.Depth(PipelineHistory); got != 1 {
		t.Fatalf("history depth after price claim = %d", got)
	}
}

func TestForwarderResumesPersistedBatchWithSameRequestID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "outbox", "state.json")
	outbox, err := NewOutbox(path)
	if err != nil {
		t.Fatal(err)
	}
	failedSender := &scriptedSender{responses: []scriptedResponse{{err: errors.New("network down")}}}
	first, err := NewForwarderWithOutbox(
		failedSender,
		observability.NewLogger(&bytes.Buffer{}, "never"),
		"west",
		outbox,
		10,
		1,
		time.Millisecond,
		1,
		time.Second,
		4,
		time.Second,
	)
	if err != nil {
		t.Fatal(err)
	}
	first.Start(context.Background())
	if !first.Enqueue(PriceIngest{ItemKey: "T4_TEST", LocationID: 4002, Quality: 1}) {
		t.Fatal("enqueue failed")
	}
	waitFor(t, 3*time.Second, func() bool {
		return first.Snapshot().Outbox.RetryingBatches == 1
	})
	firstPayloads := failedSender.Payloads()
	first.Stop()
	if len(firstPayloads) != 1 {
		t.Fatalf("first payloads = %d", len(firstPayloads))
	}

	reopened, err := NewOutbox(path)
	if err != nil {
		t.Fatal(err)
	}
	successSender := &scriptedSender{responses: []scriptedResponse{{result: SendResult{
		StatusCode: 202,
		Response:   IngestPricesResponse{Accepted: 1},
	}}}}
	second, err := NewForwarderWithOutbox(
		successSender,
		observability.NewLogger(&bytes.Buffer{}, "never"),
		"west",
		reopened,
		10,
		1,
		time.Millisecond,
		1,
		time.Millisecond,
		4,
		10*time.Millisecond,
	)
	if err != nil {
		t.Fatal(err)
	}
	second.Start(context.Background())
	waitFor(t, 3*time.Second, func() bool { return second.Snapshot().Totals.BatchesSent == 1 })
	second.Stop()
	secondPayloads := successSender.Payloads()
	if len(secondPayloads) != 1 {
		t.Fatalf("second payloads = %d", len(secondPayloads))
	}
	if firstPayloads[0].RequestID != secondPayloads[0].RequestID {
		t.Fatalf("request ID changed: %s != %s", firstPayloads[0].RequestID, secondPayloads[0].RequestID)
	}
	if reopened.Depth(PipelinePrices) != 0 {
		t.Fatalf("outbox depth = %d", reopened.Depth(PipelinePrices))
	}
}

func TestForwarderMovesPermanentFailureToDeadLetter(t *testing.T) {
	outbox, err := NewOutbox(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	sender := &scriptedSender{responses: []scriptedResponse{{err: &SendError{StatusCode: 401, Message: "unauthorized"}}}}
	forwarder, err := NewForwarderWithOutbox(
		sender,
		observability.NewLogger(&bytes.Buffer{}, "never"),
		"west",
		outbox,
		10,
		1,
		time.Millisecond,
		3,
		time.Millisecond,
		10,
		time.Second,
	)
	if err != nil {
		t.Fatal(err)
	}
	forwarder.Start(context.Background())
	forwarder.Enqueue(PriceIngest{ItemKey: "T4_TEST", LocationID: 4002, Quality: 1})
	waitFor(t, time.Second, func() bool { return forwarder.Snapshot().Outbox.DeadLetterBatches == 1 })
	forwarder.Stop()
	if sender.CallCount() != 1 {
		t.Fatalf("permanent failure calls = %d", sender.CallCount())
	}
	snapshot := forwarder.Snapshot()
	if snapshot.Totals.DeadLetterBatches != 1 || snapshot.Totals.EntriesDroppedAfterRetry != 1 {
		t.Fatalf("totals = %+v", snapshot.Totals)
	}
	letters := outbox.ListDeadLetters(PipelinePrices)
	if len(letters) != 1 || letters[0].LastStatusCode != 401 {
		t.Fatalf("letters = %+v", letters)
	}
}

func TestOutboxEnqueuesPriceUploadAtomicallyWithinCapacity(t *testing.T) {
	outbox, err := NewOutbox(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	entries := []PriceIngest{
		{ItemKey: "T4_ONE", LocationID: 4002, Quality: 1},
		{ItemKey: "T4_TWO", LocationID: 4002, Quality: 1},
		{ItemKey: "T4_THREE", LocationID: 4002, Quality: 1},
	}
	accepted, depth, err := outbox.EnqueuePrices("west", entries, 2)
	if err != nil {
		t.Fatal(err)
	}
	if accepted != 2 || depth != 2 || outbox.Depth(PipelinePrices) != 2 {
		t.Fatalf("accepted=%d depth=%d persisted=%d", accepted, depth, outbox.Depth(PipelinePrices))
	}
	accepted, depth, err = outbox.EnqueuePrices("west", entries[2:], 2)
	if !errors.Is(err, ErrOutboxFull) || accepted != 0 || depth != 2 {
		t.Fatalf("accepted=%d depth=%d err=%v", accepted, depth, err)
	}
}

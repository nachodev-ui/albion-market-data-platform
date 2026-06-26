package main

import (
	"testing"
	"time"

	"albion-market-data/collector/internal/upstream"
)

func TestDeterministicRequestIDIsStableAndUUIDShaped(t *testing.T) {
	payload := upstream.IngestHistoryRequest{
		Server: "west",
		Entries: []upstream.HistoryIngest{{
			ObservedAt: time.Date(2026, 6, 26, 0, 0, 0, 0, time.UTC),
			LocationID: 4002,
			ItemKey:    "T4_TEST",
			Quality:    1,
			History: []upstream.HistoryBucketIngest{{
				Timestamp: time.Date(2026, 6, 25, 0, 0, 0, 0, time.UTC),
				ItemCount: 2,
			}},
		}},
	}
	first := deterministicRequestID(payload)
	second := deterministicRequestID(payload)
	if first != second {
		t.Fatalf("request IDs differ: %s != %s", first, second)
	}
	if len(first) != 36 || first[8] != '-' || first[13] != '-' || first[18] != '-' || first[23] != '-' {
		t.Fatalf("not UUID shaped: %q", first)
	}
	payload.Entries[0].History[0].ItemCount = 3
	if changed := deterministicRequestID(payload); changed == first {
		t.Fatalf("payload change did not change request ID: %s", changed)
	}
}

func TestBatchBoundaryHonorsEntryAndBucketLimits(t *testing.T) {
	entries := []upstream.HistoryIngest{
		{History: make([]upstream.HistoryBucketIngest, 2)},
		{History: make([]upstream.HistoryBucketIngest, 2)},
		{History: make([]upstream.HistoryBucketIngest, 1)},
	}
	count, buckets := batchBoundary(entries, 10, 3)
	if count != 1 || buckets != 2 {
		t.Fatalf("count=%d buckets=%d", count, buckets)
	}
	count, buckets = batchBoundary(entries, 2, 10)
	if count != 2 || buckets != 4 {
		t.Fatalf("count=%d buckets=%d", count, buckets)
	}
}

func TestRowsTouchedForAttemptDistinguishesDuplicateReplay(t *testing.T) {
	original, current := rowsTouchedForAttempt(true, 3501)
	if original != 3501 || current != 0 {
		t.Fatalf("duplicate original=%d current=%d", original, current)
	}

	original, current = rowsTouchedForAttempt(false, 3501)
	if original != 3501 || current != 3501 {
		t.Fatalf("new ingest original=%d current=%d", original, current)
	}
}

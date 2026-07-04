package observability

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestRegistryRecordsReceiverAndStorageMetrics(t *testing.T) {
	startedAt := time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC)
	current := startedAt.Add(42 * time.Second)
	registry := NewRegistry(startedAt)
	registry.now = func() time.Time { return current }

	registry.RecordCapture("marketorders.ingest", 120)
	registry.RecordEntriesReceived("prices", 3)
	registry.RecordPersistence("prices", 2, 1)
	registry.RecordNormalizationError("history")
	registry.RecordStorageSuccess("raw")
	registry.RecordStorageError("outbox", errors.New("temporary write failure"))

	snapshot := registry.Snapshot()
	if snapshot.UptimeSeconds != 42 {
		t.Fatalf("uptime=%d", snapshot.UptimeSeconds)
	}
	if snapshot.CapturesReceived["marketorders.ingest"].Total != 1 || snapshot.CaptureBytes["marketorders.ingest"] != 120 {
		t.Fatalf("capture metrics=%+v bytes=%v", snapshot.CapturesReceived, snapshot.CaptureBytes)
	}
	if snapshot.EntriesReceived["prices"].Total != 3 || snapshot.EntriesStored["prices"].Total != 2 || snapshot.Duplicates["prices"].Total != 1 {
		t.Fatalf("entry metrics=%+v stored=%+v duplicates=%+v", snapshot.EntriesReceived, snapshot.EntriesStored, snapshot.Duplicates)
	}
	if snapshot.NormalizationErrors["history"].Total != 1 {
		t.Fatalf("normalization=%+v", snapshot.NormalizationErrors)
	}
	if snapshot.Storage["raw"].WritesTotal != 1 || snapshot.Storage["outbox"].ErrorsTotal != 1 {
		t.Fatalf("storage=%+v", snapshot.Storage)
	}
}

func TestRegistrySanitizesLongStorageErrors(t *testing.T) {
	registry := NewRegistry(time.Now())
	registry.RecordStorageError("database", errors.New(strings.Repeat("x ", 600)))
	snapshot := registry.Snapshot()
	if len(snapshot.Storage["database"].LastError) > maxRegistryErrorLength+3 {
		t.Fatalf("error was not bounded: %d", len(snapshot.Storage["database"].LastError))
	}
}

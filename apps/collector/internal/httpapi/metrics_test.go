package httpapi

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"albion-market-data/collector/internal/observability"
	"albion-market-data/collector/internal/upstream"
)

func TestMetricsEndpointExposesReceiverForwarderAndStorageMetrics(t *testing.T) {
	startedAt := time.Now().UTC().Add(-30 * time.Second)
	registry := observability.NewRegistry(startedAt)
	registry.RecordCapture("marketorders.ingest", 321)
	registry.RecordEntriesReceived("prices", 3)
	registry.RecordPersistence("prices", 2, 1)
	registry.RecordNormalizationError("history")
	registry.RecordStorageSuccess("raw")

	root := t.TempDir()
	raw := filepath.Join(root, "raw")
	normalized := filepath.Join(root, "normalized")
	if err := os.MkdirAll(raw, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(normalized, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(raw, "raw.jsonl"), []byte("12345"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(normalized, "orders.jsonl"), []byte("1234567"), 0o600); err != nil {
		t.Fatal(err)
	}
	lastSuccess := time.Date(2026, 7, 4, 15, 0, 0, 0, time.UTC)
	price := fakeForwarderStatus{snapshot: upstream.ForwarderSnapshot{
		Enabled: true,
		Running: true,
		Status:  "ok",
		Queue: upstream.QueueSnapshot{
			Depth:    4,
			Capacity: 5000,
		},
		Outbox: upstream.OutboxPipelineSnapshot{
			OldestPendingAgeSeconds: 12,
			DeadLetterBatches:       1,
			DeadLetterBatchesTotal:  2,
			RecoveredBatchesTotal:   3,
		},
		Totals: upstream.TotalsSnapshot{
			BatchesSent:      9,
			Retries:          2,
			RecoveredBatches: 1,
		},
		LatencyMS:     upstream.LatencySnapshot{LastBatchMS: 125},
		LastSuccessAt: &lastSuccess,
	}}
	history := fakeHistoryForwarderStatus{snapshot: upstream.HistoryForwarderSnapshot{
		Enabled: true,
		Running: true,
		Status:  "idle",
		Queue:   upstream.QueueSnapshot{Capacity: 1000},
	}}

	handler := NewMetricsHandler(MetricsConfig{
		Registry: registry,
		Storage: observability.NewStorageUsage(observability.StoragePaths{
			RawDirectory:        raw,
			NormalizedDirectory: normalized,
			DatabasePath:        filepath.Join(root, "database.json"),
			OutboxPath:          filepath.Join(root, "outbox.json"),
			MaxBytes:            1000,
		}, time.Second),
		Build: observability.BuildInfo{
			Version:   "v1.2.3",
			Commit:    "abc123",
			BuiltAt:   "2026-07-04T15:00:00Z",
			GoVersion: "go1.23.2",
		},
		Forwarder:        price,
		HistoryForwarder: history,
	})

	request := httptest.NewRequest(http.MethodGet, RouteMetrics, nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if contentType := response.Header().Get("Content-Type"); !strings.Contains(contentType, "text/plain") {
		t.Fatalf("content type=%q", contentType)
	}
	body := response.Body.String()
	checks := []string{
		`albion_receiver_build_info{built_at="2026-07-04T15:00:00Z",commit="abc123",go_version="go1.23.2",modified="false",version="v1.2.3"} 1`,
		`albion_receiver_captures_received_total{topic="marketorders.ingest"} 1`,
		`albion_receiver_entries_received_total{pipeline="prices"} 3`,
		`albion_receiver_entries_stored_total{pipeline="prices"} 2`,
		`albion_receiver_duplicates_total{pipeline="prices"} 1`,
		`albion_receiver_normalization_errors_total{pipeline="history"} 1`,
		`albion_receiver_storage_bytes{area="raw"} 5`,
		`albion_receiver_outbox_depth{pipeline="prices"} 4`,
		`albion_receiver_outbox_capacity{pipeline="prices"} 5000`,
		`albion_receiver_dead_letter_batches_total{pipeline="prices"} 2`,
		`albion_receiver_forwarder_retries_total{pipeline="prices"} 2`,
		`albion_receiver_upstream_latency_seconds{pipeline="prices",stat="last_batch"} 0.125000`,
		`albion_receiver_upstream_last_success_timestamp_seconds{pipeline="prices"} 1783177200`,
	}
	for _, check := range checks {
		if !strings.Contains(body, check) {
			t.Errorf("missing %q in metrics:\n%s", check, body)
		}
	}
}

func TestMetricsEndpointRejectsNonGET(t *testing.T) {
	handler := NewMetricsHandler(MetricsConfig{})
	request := httptest.NewRequest(http.MethodPost, RouteMetrics, nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusMethodNotAllowed || response.Header().Get("Allow") != "GET" {
		t.Fatalf("status=%d allow=%q", response.Code, response.Header().Get("Allow"))
	}
}

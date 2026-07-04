package httpingest

import (
	"bytes"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"albion-market-data/collector/internal/observability"
)

func TestHandlerRecordsReceiverMetrics(t *testing.T) {
	metrics := observability.NewRegistry(time.Now().UTC())
	rawStore := &fakeRawStore{}
	normalizedStore := &fakeNormalizedStore{}
	handler, err := NewHandlerWithOptions(
		"west",
		rawStore,
		newTestNormalizer(t, normalizedStore),
		nil,
		nil,
		log.New(io.Discard, "", 0),
		Options{Metrics: metrics},
	)
	if err != nil {
		t.Fatal(err)
	}
	handler.now = func() time.Time { return time.Date(2026, 7, 4, 15, 0, 0, 0, time.UTC) }
	body := []byte(`{"Orders":[{"Id":15083190320,"ItemTypeId":"T4_MAIN_CURSEDSTAFF_CRYSTAL@4","ItemGroupTypeId":"T4_MAIN_CURSEDSTAFF_CRYSTAL","LocationId":"1002","QualityLevel":2,"EnchantmentLevel":4,"UnitPriceSilver":9099970000,"Amount":1,"AuctionType":"offer","Expires":"2026-07-22T18:01:45.98371"}]}`)

	for range 2 {
		request := httptest.NewRequest(http.MethodPost, "/marketorders.ingest", bytes.NewReader(body))
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
		}
	}
	invalidRequest := httptest.NewRequest(http.MethodPost, "/markethistories.ingest", bytes.NewBufferString(`{"invalid":`))
	invalidResponse := httptest.NewRecorder()
	handler.ServeHTTP(invalidResponse, invalidRequest)
	if invalidResponse.Code != http.StatusBadRequest {
		t.Fatalf("invalid status=%d", invalidResponse.Code)
	}

	snapshot := metrics.Snapshot()
	if snapshot.CapturesReceived["marketorders.ingest"].Total != 2 || snapshot.CapturesReceived["markethistories.ingest"].Total != 1 {
		t.Fatalf("captures=%+v", snapshot.CapturesReceived)
	}
	if snapshot.EntriesReceived["prices"].Total != 2 || snapshot.EntriesStored["prices"].Total != 1 || snapshot.Duplicates["prices"].Total != 1 {
		t.Fatalf("received=%+v stored=%+v duplicates=%+v", snapshot.EntriesReceived, snapshot.EntriesStored, snapshot.Duplicates)
	}
	if snapshot.NormalizationErrors["history"].Total != 1 {
		t.Fatalf("normalization errors=%+v", snapshot.NormalizationErrors)
	}
}

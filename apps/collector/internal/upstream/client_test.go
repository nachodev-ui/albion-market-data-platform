package upstream

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClientSendsAuthenticatedGzipPayloadAndDecodesResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/ingest/prices" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		if r.Header.Get("Content-Encoding") != "gzip" {
			t.Fatalf("content encoding = %q", r.Header.Get("Content-Encoding"))
		}
		reader, err := gzip.NewReader(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		defer reader.Close()
		content, err := io.ReadAll(reader)
		if err != nil {
			t.Fatal(err)
		}
		var payload IngestPricesRequest
		if err := json.Unmarshal(content, &payload); err != nil {
			t.Fatal(err)
		}
		if payload.RequestID != "request-1" || len(payload.Entries) != 1 {
			t.Fatalf("payload = %+v", payload)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = io.WriteString(w, `{"request_id":"request-1","accepted":1,"current_rows_touched":1,"duplicate":false}`)
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "secret", time.Second, true)
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.SendPrices(context.Background(), IngestPricesRequest{
		RequestID: "request-1",
		Server:    "west",
		Entries:   []PriceIngest{{ItemKey: "T4_TEST", LocationID: 4002, Quality: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.StatusCode != http.StatusAccepted || result.Response.Accepted != 1 || result.Response.CurrentRowsTouched != 1 {
		t.Fatalf("result = %+v", result)
	}
	if result.Duration <= 0 {
		t.Fatalf("duration = %s", result.Duration)
	}
}

func TestClientReturnsStatusAndLatencyOnHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, `{"error":"temporarily unavailable"}`)
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "secret", time.Second, false)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.SendPrices(context.Background(), IngestPricesRequest{RequestID: "request-1", Server: "west"})
	if err == nil {
		t.Fatal("expected error")
	}
	status, duration := SendErrorDetails(err)
	if status != http.StatusServiceUnavailable || duration <= 0 {
		t.Fatalf("status=%d duration=%s error=%v", status, duration, err)
	}
}

func TestClientSendsHistoryPayloadAndDecodesResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/ingest/history" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		var payload IngestHistoryRequest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload.RequestID != "00112233-4455-6677-8899-aabbccddeeff" || len(payload.Entries) != 1 || len(payload.Entries[0].History) != 1 {
			t.Fatalf("payload = %+v", payload)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = io.WriteString(w, `{"request_id":"00112233-4455-6677-8899-aabbccddeeff","accepted_entries":1,"accepted_buckets":1,"history_rows_touched":1,"duplicate":false}`)
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "secret", time.Second, false)
	if err != nil {
		t.Fatal(err)
	}
	average := int64(18750)
	result, err := client.SendHistory(context.Background(), IngestHistoryRequest{
		RequestID: "00112233-4455-6677-8899-aabbccddeeff",
		Server:    "west",
		Entries: []HistoryIngest{{
			ObservedAt: time.Date(2026, 6, 26, 20, 0, 0, 0, time.UTC),
			LocationID: 4002,
			ItemKey:    "T5_LEATHER_LEVEL4@4",
			Quality:    1,
			History: []HistoryBucketIngest{{
				Timestamp:        time.Date(2026, 6, 25, 0, 0, 0, 0, time.UTC),
				ItemCount:        42,
				AverageUnitPrice: &average,
			}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.StatusCode != http.StatusAccepted || result.Response.AcceptedEntries != 1 || result.Response.AcceptedBuckets != 1 || result.Response.HistoryRowsTouched != 1 {
		t.Fatalf("result = %+v", result)
	}
}

func TestClientRequiresHTTPSWhenConfigured(t *testing.T) {
	t.Parallel()

	if _, err := NewClientWithOptions(ClientOptions{
		BaseURL:      "http://api.example.test",
		Token:        "secret",
		RequireHTTPS: true,
	}); err == nil {
		t.Fatal("expected HTTPS requirement error")
	}
	if _, err := NewClientWithOptions(ClientOptions{
		BaseURL:      "https://api.example.test",
		Token:        "secret",
		RequireHTTPS: true,
	}); err != nil {
		t.Fatalf("https client: %v", err)
	}
}

func TestClientRejectsCredentialedURL(t *testing.T) {
	t.Parallel()

	if _, err := NewClient("https://user:password@api.example.test", "secret", time.Second, false); err == nil {
		t.Fatal("expected credentialed URL error")
	}
}

func TestClientDoesNotForwardBearerTokenAcrossRedirects(t *testing.T) {
	t.Parallel()

	targetCalled := false
	target := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		targetCalled = true
	}))
	defer target.Close()

	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", target.URL)
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()

	client, err := NewClient(redirect.URL, "secret", time.Second, false)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.SendPrices(context.Background(), IngestPricesRequest{RequestID: "request-1", Server: "west"})
	if err == nil {
		t.Fatal("expected redirect response to be rejected")
	}
	if targetCalled {
		t.Fatal("redirect target received the request")
	}
}

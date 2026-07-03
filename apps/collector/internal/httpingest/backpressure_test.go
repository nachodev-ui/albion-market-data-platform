package httpingest

import (
	"bytes"
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"

	"albion-market-data/collector/internal/domain"
)

type blockingRawStore struct {
	started chan struct{}
	release chan struct{}
}

func (s *blockingRawStore) AppendRaw(ctx context.Context, _ domain.RawIngestEvent) error {
	select {
	case s.started <- struct{}{}:
	default:
	}
	select {
	case <-s.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestHandlerRejectsExcessConcurrentIngest(t *testing.T) {
	rawStore := &blockingRawStore{started: make(chan struct{}, 1), release: make(chan struct{})}
	handler, err := NewHandlerWithOptions(
		"west",
		rawStore,
		newTestNormalizer(t, &fakeNormalizedStore{}),
		nil,
		nil,
		log.New(io.Discard, "", 0),
		Options{MaxConcurrent: 1},
	)
	if err != nil {
		t.Fatal(err)
	}

	firstDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		request := httptest.NewRequest(http.MethodPost, "/other.ingest", bytes.NewBufferString(`{"value":1}`))
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		firstDone <- response
	}()
	<-rawStore.started

	secondRequest := httptest.NewRequest(http.MethodPost, "/other.ingest", bytes.NewBufferString(`{"value":2}`))
	secondResponse := httptest.NewRecorder()
	handler.ServeHTTP(secondResponse, secondRequest)
	if secondResponse.Code != http.StatusTooManyRequests {
		t.Fatalf("status=%d body=%s", secondResponse.Code, secondResponse.Body.String())
	}
	if secondResponse.Header().Get("Retry-After") != "1" {
		t.Fatalf("Retry-After=%q", secondResponse.Header().Get("Retry-After"))
	}

	close(rawStore.release)
	firstResponse := <-firstDone
	if firstResponse.Code != http.StatusOK {
		t.Fatalf("first status=%d body=%s", firstResponse.Code, firstResponse.Body.String())
	}
}

package httpingest

import (
	"bytes"
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"albion-market-data/collector/internal/catalog"
	"albion-market-data/collector/internal/domain"
	"albion-market-data/collector/internal/normalization"
)

type countingRawStore struct {
	mu     sync.Mutex
	events []domain.RawIngestEvent
}

func (s *countingRawStore) AppendRaw(_ context.Context, event domain.RawIngestEvent) error {
	s.mu.Lock()
	s.events = append(s.events, event)
	s.mu.Unlock()
	return nil
}

func (s *countingRawStore) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.events)
}

type blockingNormalizedStore struct {
	mu           sync.Mutex
	orderCalls   int
	firstStarted chan struct{}
	releaseFirst chan struct{}
}

func (s *blockingNormalizedStore) AppendRaw(_ context.Context, _ domain.RawIngestEvent) error {
	return nil
}

func (s *blockingNormalizedStore) AppendHistory(_ context.Context, _ domain.NormalizedHistory) (bool, error) {
	return true, nil
}

func (s *blockingNormalizedStore) AppendOrders(ctx context.Context, orders []domain.NormalizedMarketOrder) (int, int, error) {
	s.mu.Lock()
	s.orderCalls++
	call := s.orderCalls
	s.mu.Unlock()

	if call == 1 {
		close(s.firstStarted)
		select {
		case <-s.releaseFirst:
		case <-ctx.Done():
			return 0, 0, ctx.Err()
		}
	}
	return len(orders), 0, nil
}

func newBackpressureNormalizer(t *testing.T, store normalization.Store) *normalization.Service {
	t.Helper()
	directory := t.TempDir()
	items := filepath.Join(directory, "items.txt")
	markets := filepath.Join(directory, "markets.json")
	if err := os.WriteFile(items, []byte("6826: T4_MAIN_CURSEDSTAFF_CRYSTAL@4 : Adept's Rotcaller Staff\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(markets, []byte(`{"schemaVersion":1,"markets":[{"key":"lymhurst","name":"Lymhurst","type":"regular","cityLocationId":"1000","marketLocationId":"1002","enabled":true}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := catalog.Load(items, markets)
	if err != nil {
		t.Fatal(err)
	}
	service, err := normalization.NewService(loaded, store)
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func TestHandlerQueuesExcessConcurrentIngestWithoutDropping(t *testing.T) {
	rawStore := &countingRawStore{}
	normalizedStore := &blockingNormalizedStore{
		firstStarted: make(chan struct{}),
		releaseFirst: make(chan struct{}),
	}
	handler, err := NewHandlerWithOptions(
		"west",
		rawStore,
		newBackpressureNormalizer(t, normalizedStore),
		nil,
		nil,
		log.New(io.Discard, "", 0),
		Options{MaxConcurrent: 1},
	)
	if err != nil {
		t.Fatal(err)
	}

	body := []byte(`{"Orders":[{"Id":15083190320,"ItemTypeId":"T4_MAIN_CURSEDSTAFF_CRYSTAL@4","ItemGroupTypeId":"T4_MAIN_CURSEDSTAFF_CRYSTAL","LocationId":"1002","QualityLevel":2,"EnchantmentLevel":4,"UnitPriceSilver":9099970000,"Amount":1,"AuctionType":"offer","Expires":"2026-07-22T18:01:45.98371"}]}`)
	firstDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		request := httptest.NewRequest(http.MethodPost, "/marketorders.ingest", bytes.NewReader(body))
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		firstDone <- response
	}()
	<-normalizedStore.firstStarted

	secondDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		request := httptest.NewRequest(http.MethodPost, "/marketorders.ingest", bytes.NewReader(body))
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		secondDone <- response
	}()

	deadline := time.Now().Add(2 * time.Second)
	for rawStore.Count() < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if rawStore.Count() != 2 {
		t.Fatalf("raw events=%d want=2", rawStore.Count())
	}

	select {
	case response := <-secondDone:
		t.Fatalf("second request completed before a processing slot was released: status=%d body=%s", response.Code, response.Body.String())
	case <-time.After(100 * time.Millisecond):
	}

	close(normalizedStore.releaseFirst)

	firstResponse := <-firstDone
	secondResponse := <-secondDone
	if firstResponse.Code != http.StatusOK {
		t.Fatalf("first status=%d body=%s", firstResponse.Code, firstResponse.Body.String())
	}
	if secondResponse.Code != http.StatusOK {
		t.Fatalf("second status=%d body=%s", secondResponse.Code, secondResponse.Body.String())
	}
	if secondResponse.Header().Get("X-Ingest-Queue-Wait-Ms") == "" {
		t.Fatal("second response did not expose the queue wait duration")
	}
}

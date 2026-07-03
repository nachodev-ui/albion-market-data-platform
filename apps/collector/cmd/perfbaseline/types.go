package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"albion-market-data/collector/internal/domain"
	"albion-market-data/collector/internal/storage/localdb"
	"albion-market-data/collector/internal/storage/normalizedjsonl"
	"albion-market-data/collector/internal/upstream"
)

type report struct {
	Mode        string            `json:"mode"`
	GeneratedAt time.Time         `json:"generated_at"`
	GoVersion   string            `json:"go_version"`
	GOOS        string            `json:"goos"`
	GOARCH      string            `json:"goarch"`
	CPU         int               `json:"logical_cpus"`
	Scenarios   []scenarioSummary `json:"scenarios"`
	Skipped     []skippedScenario `json:"skipped,omitempty"`
}

type skippedScenario struct {
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

type scenarioSummary struct {
	Name            string           `json:"name"`
	Samples         int              `json:"samples"`
	TotalMS         float64          `json:"total_ms"`
	MeanMS          float64          `json:"mean_ms"`
	P50MS           float64          `json:"p50_ms"`
	P95MS           float64          `json:"p95_ms"`
	MinMS           float64          `json:"min_ms"`
	MaxMS           float64          `json:"max_ms"`
	AllocBytesPerOp float64          `json:"alloc_bytes_per_op"`
	AllocsPerOp     float64          `json:"allocs_per_op"`
	ArtifactsBytes  map[string]int64 `json:"artifacts_bytes,omitempty"`
	Counters        map[string]int64 `json:"counters,omitempty"`
}

type sampleDetails struct {
	Artifacts map[string]int64
	Counters  map[string]int64
}

type sampleFn func() (sampleDetails, error)

type discardStore struct{}

func (discardStore) AppendHistory(context.Context, domain.NormalizedHistory) (bool, error) {
	return true, nil
}
func (discardStore) AppendOrders(_ context.Context, orders []domain.NormalizedMarketOrder) (int, int, error) {
	return len(orders), 0, nil
}

type rawFileStore struct{ path string }

func (s rawFileStore) AppendRaw(_ context.Context, event domain.RawIngestEvent) error {
	encoded, err := json.Marshal(event)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(s.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err := file.Write(append(encoded, '\n')); err != nil {
		return err
	}
	return file.Sync()
}

type compositeStore struct {
	jsonl *normalizedjsonl.Store
	db    *localdb.Store
}

func (s compositeStore) AppendHistory(ctx context.Context, history domain.NormalizedHistory) (bool, error) {
	stored, err := s.jsonl.AppendHistory(ctx, history)
	if err != nil {
		return false, err
	}
	if _, err := s.db.AppendHistory(ctx, history); err != nil {
		return false, err
	}
	return stored, nil
}
func (s compositeStore) AppendOrders(ctx context.Context, orders []domain.NormalizedMarketOrder) (int, int, error) {
	written, duplicates, err := s.jsonl.AppendOrders(ctx, orders)
	if err != nil {
		return 0, 0, err
	}
	if _, _, err := s.db.AppendOrders(ctx, orders); err != nil {
		return 0, 0, err
	}
	return written, duplicates, nil
}

type flakySender struct{ calls atomic.Int64 }

func (s *flakySender) SendPrices(_ context.Context, request upstream.IngestPricesRequest) (upstream.SendResult, error) {
	if s.calls.Add(1) == 1 {
		return upstream.SendResult{}, errors.New("simulated upstream outage")
	}
	return upstream.SendResult{StatusCode: http.StatusAccepted, Response: upstream.IngestPricesResponse{
		RequestID: request.RequestID, Accepted: len(request.Entries),
	}}, nil
}

package rawjsonl

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"

	"albion-market-data/collector/internal/domain"
	"albion-market-data/collector/internal/storage/durable"
)

type Store struct {
	directory string
	mu        sync.Mutex
}

func NewStore(directory string) (*Store, error) {
	if directory == "" {
		return nil, fmt.Errorf("data directory is required")
	}
	if _, err := durable.RepairJSONLPatterns(directory, 20<<20, "raw-ingest-*.jsonl"); err != nil {
		return nil, fmt.Errorf("repair raw JSONL: %w", err)
	}
	return &Store{directory: directory}, nil
}

func (s *Store) AppendRaw(ctx context.Context, event domain.RawIngestEvent) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	if err := event.Validate(); err != nil {
		return fmt.Errorf("validate raw event: %w", err)
	}
	filename := "raw-ingest-" + event.ReceivedAt.UTC().Format("2006-01-02") + ".jsonl"
	path := filepath.Join(s.directory, filename)
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := durable.AppendJSONLine(path, event); err != nil {
		return fmt.Errorf("append %s: %w", path, err)
	}
	return nil
}

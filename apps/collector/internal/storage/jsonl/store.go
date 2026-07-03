package jsonl

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
	if _, err := durable.RepairJSONLPatterns(directory, 20<<20, "market-history-*.jsonl"); err != nil {
		return nil, fmt.Errorf("repair history JSONL: %w", err)
	}
	return &Store{directory: directory}, nil
}

func (s *Store) Append(ctx context.Context, capture domain.CapturedHistory) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	filename := "market-history-" + capture.CapturedAt.UTC().Format("2006-01-02") + ".jsonl"
	path := filepath.Join(s.directory, filename)
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := durable.AppendJSONLine(path, capture); err != nil {
		return fmt.Errorf("append %s: %w", path, err)
	}
	return nil
}

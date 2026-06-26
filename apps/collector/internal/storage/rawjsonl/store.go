package rawjsonl

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"albion-market-data/collector/internal/domain"
)

type Store struct {
	directory string
	mu        sync.Mutex
}

func NewStore(directory string) (*Store, error) {
	if directory == "" {
		return nil, fmt.Errorf("data directory is required")
	}
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return nil, fmt.Errorf("create data directory: %w", err)
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

	encoded, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("encode raw event: %w", err)
	}

	filename := "raw-ingest-" + event.ReceivedAt.UTC().Format("2006-01-02") + ".jsonl"
	path := filepath.Join(s.directory, filename)

	s.mu.Lock()
	defer s.mu.Unlock()

	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer file.Close()

	if _, err := file.Write(append(encoded, '\n')); err != nil {
		return fmt.Errorf("append %s: %w", path, err)
	}
	return nil
}

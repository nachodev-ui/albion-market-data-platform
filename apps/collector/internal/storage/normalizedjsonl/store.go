package normalizedjsonl

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"albion-market-data/collector/internal/domain"
	"albion-market-data/collector/internal/storage/durable"
)

type Store struct {
	directory   string
	mu          sync.Mutex
	historyKeys map[string]struct{}
	orderKeys   map[string]struct{}
}

func NewStore(directory string) (*Store, error) {
	if directory == "" {
		return nil, fmt.Errorf("normalized data directory is required")
	}
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return nil, fmt.Errorf("create normalized data directory: %w", err)
	}
	if _, err := durable.RepairJSONLPatterns(directory, 20<<20, "market-history-*.jsonl", "market-orders-*.jsonl"); err != nil {
		return nil, fmt.Errorf("repair normalized JSONL: %w", err)
	}
	store := &Store{
		directory:   directory,
		historyKeys: make(map[string]struct{}),
		orderKeys:   make(map[string]struct{}),
	}
	if err := store.loadExistingKeys(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *Store) AppendHistory(ctx context.Context, history domain.NormalizedHistory) (bool, error) {
	if err := history.Validate(); err != nil {
		return false, fmt.Errorf("validate history: %w", err)
	}
	select {
	case <-ctx.Done():
		return false, ctx.Err()
	default:
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.historyKeys[history.DedupeKey]; exists {
		return false, nil
	}
	filename := "market-history-" + history.CapturedAt.UTC().Format("2006-01-02") + ".jsonl"
	if err := appendJSON(filepath.Join(s.directory, filename), history); err != nil {
		return false, err
	}
	s.historyKeys[history.DedupeKey] = struct{}{}
	return true, nil
}

func (s *Store) AppendOrders(ctx context.Context, orders []domain.NormalizedMarketOrder) (int, int, error) {
	select {
	case <-ctx.Done():
		return 0, 0, ctx.Err()
	default:
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	written := 0
	duplicates := 0
	for _, order := range orders {
		if err := order.Validate(); err != nil {
			return written, duplicates, fmt.Errorf("validate order %d: %w", order.OrderID, err)
		}
		if _, exists := s.orderKeys[order.DedupeKey]; exists {
			duplicates++
			continue
		}
		filename := "market-orders-" + order.CapturedAt.UTC().Format("2006-01-02") + ".jsonl"
		if err := appendJSON(filepath.Join(s.directory, filename), order); err != nil {
			return written, duplicates, err
		}
		s.orderKeys[order.DedupeKey] = struct{}{}
		written++
	}
	return written, duplicates, nil
}

func (s *Store) loadExistingKeys() error {
	if err := s.scanKeys("market-history-*.jsonl", func(line []byte) error {
		var record struct {
			DedupeKey string `json:"dedupeKey"`
		}
		if err := json.Unmarshal(line, &record); err != nil {
			return err
		}
		if record.DedupeKey != "" {
			s.historyKeys[record.DedupeKey] = struct{}{}
		}
		return nil
	}); err != nil {
		return fmt.Errorf("load existing history keys: %w", err)
	}
	if err := s.scanKeys("market-orders-*.jsonl", func(line []byte) error {
		var record struct {
			DedupeKey string `json:"dedupeKey"`
		}
		if err := json.Unmarshal(line, &record); err != nil {
			return err
		}
		if record.DedupeKey != "" {
			s.orderKeys[record.DedupeKey] = struct{}{}
		}
		return nil
	}); err != nil {
		return fmt.Errorf("load existing order keys: %w", err)
	}
	return nil
}

func (s *Store) scanKeys(pattern string, visit func([]byte) error) error {
	paths, err := filepath.Glob(filepath.Join(s.directory, pattern))
	if err != nil {
		return err
	}
	for _, path := range paths {
		file, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("open %s: %w", path, err)
		}
		scanner := bufio.NewScanner(file)
		scanner.Buffer(make([]byte, 64*1024), 20<<20)
		line := 0
		for scanner.Scan() {
			line++
			if err := visit(scanner.Bytes()); err != nil {
				file.Close()
				return fmt.Errorf("decode %s line %d: %w", path, line, err)
			}
		}
		if err := scanner.Err(); err != nil {
			file.Close()
			return fmt.Errorf("scan %s: %w", path, err)
		}
		if err := file.Close(); err != nil {
			return fmt.Errorf("close %s: %w", path, err)
		}
	}
	return nil
}

func appendJSON(path string, value any) error {
	if err := durable.AppendJSONLine(path, value); err != nil {
		return fmt.Errorf("append normalized record: %w", err)
	}
	return nil
}

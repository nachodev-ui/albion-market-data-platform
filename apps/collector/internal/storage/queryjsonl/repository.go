package queryjsonl

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"albion-market-data/collector/internal/domain"
)

type Repository struct {
	directory string
}

type HistoryFilter struct {
	Server       string
	ItemID       string
	LocationID   string
	LocationName string
	Quality      uint8
	Period       string
	Limit        int
}

type OrderFilter struct {
	Server       string
	ItemID       string
	LocationID   string
	LocationName string
	Quality      uint8
	AuctionType  string
	Side         string
	Limit        int
}

type RepositoryStats struct {
	HistorySnapshots int       `json:"historySnapshots"`
	OrderSnapshots   int       `json:"orderSnapshots"`
	UpdatedAt        time.Time `json:"updatedAt,omitempty"`
	Storage          string    `json:"storage"`
}

func NewRepository(directory string) (*Repository, error) {
	if strings.TrimSpace(directory) == "" {
		return nil, fmt.Errorf("normalized data directory is required")
	}
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return nil, fmt.Errorf("create normalized data directory: %w", err)
	}
	return &Repository{directory: directory}, nil
}

func (r *Repository) ListHistories(ctx context.Context, filter HistoryFilter) ([]domain.NormalizedHistory, error) {
	filter.Limit = normalizeLimit(filter.Limit)
	var records []domain.NormalizedHistory
	err := r.scan(ctx, "market-history-*.jsonl", func(line []byte) error {
		var record domain.NormalizedHistory
		if err := json.Unmarshal(line, &record); err != nil {
			return err
		}
		if !matchesHistory(record, filter) {
			return nil
		}
		records = append(records, record)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(records, func(i, j int) bool {
		return records[i].CapturedAt.After(records[j].CapturedAt)
	})
	if len(records) > filter.Limit {
		records = records[:filter.Limit]
	}
	return records, nil
}

func (r *Repository) ListOrders(ctx context.Context, filter OrderFilter) ([]domain.NormalizedMarketOrder, error) {
	filter.Limit = normalizeLimit(filter.Limit)
	var records []domain.NormalizedMarketOrder
	err := r.scan(ctx, "market-orders-*.jsonl", func(line []byte) error {
		var record domain.NormalizedMarketOrder
		if err := json.Unmarshal(line, &record); err != nil {
			return err
		}
		if !matchesOrder(record, filter) {
			return nil
		}
		records = append(records, record)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return latestOrders(records, filter.Limit), nil
}

func matchesHistory(record domain.NormalizedHistory, filter HistoryFilter) bool {
	if filter.Server != "" && record.Server != filter.Server {
		return false
	}
	if filter.ItemID != "" && record.Item.ID != filter.ItemID {
		return false
	}
	if filter.LocationID != "" && record.Location.ID != filter.LocationID {
		return false
	}
	if filter.LocationName != "" && !strings.EqualFold(record.Location.Name, filter.LocationName) {
		return false
	}
	if filter.Quality != 0 && record.Quality.ID != filter.Quality {
		return false
	}
	return filter.Period == "" || record.Period == filter.Period
}

func matchesOrder(record domain.NormalizedMarketOrder, filter OrderFilter) bool {
	if filter.Server != "" && record.Server != filter.Server {
		return false
	}
	if filter.ItemID != "" && record.Item.ID != filter.ItemID {
		return false
	}
	if filter.LocationID != "" && record.Location.ID != filter.LocationID {
		return false
	}
	if filter.LocationName != "" && !strings.EqualFold(record.Location.Name, filter.LocationName) {
		return false
	}
	if filter.Quality != 0 && record.Quality.ID != filter.Quality {
		return false
	}
	if filter.AuctionType != "" && record.AuctionType != filter.AuctionType {
		return false
	}
	return filter.Side == "" || record.Side == filter.Side
}

func latestOrders(records []domain.NormalizedMarketOrder, limit int) []domain.NormalizedMarketOrder {
	sort.Slice(records, func(i, j int) bool {
		if records[i].CapturedAt.Equal(records[j].CapturedAt) {
			return records[i].UnitPrice < records[j].UnitPrice
		}
		return records[i].CapturedAt.After(records[j].CapturedAt)
	})

	latest := make([]domain.NormalizedMarketOrder, 0, min(len(records), limit))
	seenOrderIDs := make(map[int64]struct{})
	for _, record := range records {
		if _, exists := seenOrderIDs[record.OrderID]; exists {
			continue
		}
		seenOrderIDs[record.OrderID] = struct{}{}
		latest = append(latest, record)
		if len(latest) == limit {
			break
		}
	}
	return latest
}

func (r *Repository) scan(ctx context.Context, pattern string, visit func([]byte) error) error {
	paths, err := filepath.Glob(filepath.Join(r.directory, pattern))
	if err != nil {
		return fmt.Errorf("glob normalized files: %w", err)
	}
	for _, path := range paths {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		file, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("open %s: %w", path, err)
		}
		scanner := bufio.NewScanner(file)
		scanner.Buffer(make([]byte, 64*1024), 10<<20)
		lineNumber := 0
		for scanner.Scan() {
			lineNumber++
			if err := visit(scanner.Bytes()); err != nil {
				file.Close()
				return fmt.Errorf("decode %s line %d: %w", path, lineNumber, err)
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

func normalizeLimit(limit int) int {
	if limit <= 0 {
		return 100
	}
	if limit > 5000 {
		return 5000
	}
	return limit
}

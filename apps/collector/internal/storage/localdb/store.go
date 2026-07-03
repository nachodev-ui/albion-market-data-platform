package localdb

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"albion-market-data/collector/internal/domain"
	"albion-market-data/collector/internal/storage/durable"
	"albion-market-data/collector/internal/storage/queryjsonl"
)

const schemaVersion = 1

type persistedState struct {
	SchemaVersion int                            `json:"schemaVersion"`
	UpdatedAt     time.Time                      `json:"updatedAt"`
	Histories     []domain.NormalizedHistory     `json:"histories"`
	Orders        []domain.NormalizedMarketOrder `json:"orders"`
}

type ImportResult struct {
	HistoryImported int `json:"historyImported"`
	OrdersImported  int `json:"ordersImported"`
}

type Store struct {
	path      string
	mu        sync.RWMutex
	histories map[string]domain.NormalizedHistory
	orders    map[string]domain.NormalizedMarketOrder
	updatedAt time.Time
}

func New(path string) (*Store, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("local database path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create local database directory: %w", err)
	}
	store := &Store{
		path:      path,
		histories: make(map[string]domain.NormalizedHistory),
		orders:    make(map[string]domain.NormalizedMarketOrder),
	}
	if err := store.load(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *Store) load() error {
	content, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read local database: %w", err)
	}
	if len(content) == 0 {
		return nil
	}
	var state persistedState
	if err := json.Unmarshal(content, &state); err != nil {
		return fmt.Errorf("decode local database: %w", err)
	}
	if state.SchemaVersion != schemaVersion {
		return fmt.Errorf("unsupported local database schema version %d", state.SchemaVersion)
	}
	for _, history := range state.Histories {
		if err := history.Validate(); err != nil {
			return fmt.Errorf("validate persisted history: %w", err)
		}
		s.histories[history.DedupeKey] = history
	}
	for _, order := range state.Orders {
		if err := order.Validate(); err != nil {
			return fmt.Errorf("validate persisted order: %w", err)
		}
		s.orders[order.DedupeKey] = order
	}
	s.updatedAt = state.UpdatedAt
	return nil
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
	if _, exists := s.histories[history.DedupeKey]; exists {
		return false, nil
	}
	s.histories[history.DedupeKey] = history
	if err := s.persistLocked(); err != nil {
		delete(s.histories, history.DedupeKey)
		return false, err
	}
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
	addedKeys := make([]string, 0, len(orders))
	for _, order := range orders {
		if err := order.Validate(); err != nil {
			return written, duplicates, fmt.Errorf("validate order %d: %w", order.OrderID, err)
		}
		if _, exists := s.orders[order.DedupeKey]; exists {
			duplicates++
			continue
		}
		s.orders[order.DedupeKey] = order
		addedKeys = append(addedKeys, order.DedupeKey)
		written++
	}
	if written == 0 {
		return 0, duplicates, nil
	}
	if err := s.persistLocked(); err != nil {
		for _, key := range addedKeys {
			delete(s.orders, key)
		}
		return 0, duplicates, err
	}
	return written, duplicates, nil
}

func (s *Store) ImportNormalizedDirectory(ctx context.Context, directory string) (ImportResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := ImportResult{}
	if err := s.importFilesLocked(ctx, filepath.Join(directory, "market-history-*.jsonl"), func(line []byte) error {
		var record domain.NormalizedHistory
		if err := json.Unmarshal(line, &record); err != nil {
			return err
		}
		if err := record.Validate(); err != nil {
			return err
		}
		if _, exists := s.histories[record.DedupeKey]; !exists {
			s.histories[record.DedupeKey] = record
			result.HistoryImported++
		}
		return nil
	}); err != nil {
		return ImportResult{}, err
	}
	if err := s.importFilesLocked(ctx, filepath.Join(directory, "market-orders-*.jsonl"), func(line []byte) error {
		var record domain.NormalizedMarketOrder
		if err := json.Unmarshal(line, &record); err != nil {
			return err
		}
		if err := record.Validate(); err != nil {
			return err
		}
		if _, exists := s.orders[record.DedupeKey]; !exists {
			s.orders[record.DedupeKey] = record
			result.OrdersImported++
		}
		return nil
	}); err != nil {
		return ImportResult{}, err
	}
	if result.HistoryImported > 0 || result.OrdersImported > 0 {
		if err := s.persistLocked(); err != nil {
			return ImportResult{}, err
		}
	}
	return result, nil
}

func (s *Store) importFilesLocked(ctx context.Context, pattern string, visit func([]byte) error) error {
	paths, err := filepath.Glob(pattern)
	if err != nil {
		return fmt.Errorf("glob normalized files: %w", err)
	}
	sort.Strings(paths)
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
		scanner.Buffer(make([]byte, 64*1024), 20<<20)
		lineNumber := 0
		for scanner.Scan() {
			lineNumber++
			if strings.TrimSpace(scanner.Text()) == "" {
				continue
			}
			if err := visit(scanner.Bytes()); err != nil {
				file.Close()
				return fmt.Errorf("import %s line %d: %w", path, lineNumber, err)
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

func (s *Store) ListHistories(ctx context.Context, filter queryjsonl.HistoryFilter) ([]domain.NormalizedHistory, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	limit := normalizeLimit(filter.Limit)
	result := make([]domain.NormalizedHistory, 0)
	for _, record := range s.histories {
		if filter.Server != "" && record.Server != filter.Server {
			continue
		}
		if filter.ItemID != "" && record.Item.ID != filter.ItemID {
			continue
		}
		if filter.LocationID != "" && record.Location.ID != filter.LocationID {
			continue
		}
		if filter.LocationName != "" && !strings.EqualFold(record.Location.Name, filter.LocationName) {
			continue
		}
		if filter.Quality != 0 && record.Quality.ID != filter.Quality {
			continue
		}
		if filter.Period != "" && record.Period != filter.Period {
			continue
		}
		result = append(result, record)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CapturedAt.After(result[j].CapturedAt) })
	if len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

func (s *Store) ListOrders(ctx context.Context, filter queryjsonl.OrderFilter) ([]domain.NormalizedMarketOrder, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	limit := normalizeLimit(filter.Limit)
	versions := make([]domain.NormalizedMarketOrder, 0)
	for _, record := range s.orders {
		if filter.Server != "" && record.Server != filter.Server {
			continue
		}
		if filter.ItemID != "" && record.Item.ID != filter.ItemID {
			continue
		}
		if filter.LocationID != "" && record.Location.ID != filter.LocationID {
			continue
		}
		if filter.LocationName != "" && !strings.EqualFold(record.Location.Name, filter.LocationName) {
			continue
		}
		if filter.Quality != 0 && record.Quality.ID != filter.Quality {
			continue
		}
		if filter.AuctionType != "" && record.AuctionType != filter.AuctionType {
			continue
		}
		if filter.Side != "" && record.Side != filter.Side {
			continue
		}
		versions = append(versions, record)
	}
	sort.Slice(versions, func(i, j int) bool {
		if versions[i].CapturedAt.Equal(versions[j].CapturedAt) {
			return versions[i].UnitPrice < versions[j].UnitPrice
		}
		return versions[i].CapturedAt.After(versions[j].CapturedAt)
	})
	result := make([]domain.NormalizedMarketOrder, 0, min(len(versions), limit))
	seen := make(map[int64]struct{})
	for _, record := range versions {
		if _, exists := seen[record.OrderID]; exists {
			continue
		}
		seen[record.OrderID] = struct{}{}
		result = append(result, record)
		if len(result) == limit {
			break
		}
	}
	return result, nil
}

func (s *Store) Stats() queryjsonl.RepositoryStats {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return queryjsonl.RepositoryStats{
		HistorySnapshots: len(s.histories),
		OrderSnapshots:   len(s.orders),
		UpdatedAt:        s.updatedAt,
		Storage:          "embedded-local-db",
	}
}

func (s *Store) persistLocked() error {
	histories := make([]domain.NormalizedHistory, 0, len(s.histories))
	for _, record := range s.histories {
		histories = append(histories, record)
	}
	sort.Slice(histories, func(i, j int) bool {
		if histories[i].CapturedAt.Equal(histories[j].CapturedAt) {
			return histories[i].DedupeKey < histories[j].DedupeKey
		}
		return histories[i].CapturedAt.Before(histories[j].CapturedAt)
	})
	orders := make([]domain.NormalizedMarketOrder, 0, len(s.orders))
	for _, record := range s.orders {
		orders = append(orders, record)
	}
	sort.Slice(orders, func(i, j int) bool {
		if orders[i].CapturedAt.Equal(orders[j].CapturedAt) {
			return orders[i].DedupeKey < orders[j].DedupeKey
		}
		return orders[i].CapturedAt.Before(orders[j].CapturedAt)
	})
	updatedAt := time.Now().UTC()
	payload := persistedState{
		SchemaVersion: schemaVersion,
		UpdatedAt:     updatedAt,
		Histories:     histories,
		Orders:        orders,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode local database: %w", err)
	}
	if err := durable.AtomicWrite(s.path, encoded, 0o644); err != nil {
		return fmt.Errorf("write local database: %w", err)
	}
	s.updatedAt = updatedAt
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

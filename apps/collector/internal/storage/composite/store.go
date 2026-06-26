package composite

import (
	"context"
	"fmt"

	"albion-market-data/collector/internal/domain"
	"albion-market-data/collector/internal/storage/localdb"
	"albion-market-data/collector/internal/storage/normalizedjsonl"
)

type Store struct {
	audit    *normalizedjsonl.Store
	database *localdb.Store
}

func New(audit *normalizedjsonl.Store, database *localdb.Store) (*Store, error) {
	if audit == nil {
		return nil, fmt.Errorf("normalized JSONL audit store is required")
	}
	if database == nil {
		return nil, fmt.Errorf("local database is required")
	}
	return &Store{audit: audit, database: database}, nil
}

func (s *Store) AppendHistory(ctx context.Context, history domain.NormalizedHistory) (bool, error) {
	auditStored, err := s.audit.AppendHistory(ctx, history)
	if err != nil {
		return false, err
	}
	databaseStored, err := s.database.AppendHistory(ctx, history)
	if err != nil {
		return false, err
	}
	return auditStored || databaseStored, nil
}

func (s *Store) AppendOrders(ctx context.Context, orders []domain.NormalizedMarketOrder) (int, int, error) {
	if _, _, err := s.audit.AppendOrders(ctx, orders); err != nil {
		return 0, 0, err
	}
	return s.database.AppendOrders(ctx, orders)
}

package ingest

import (
	"context"
	"fmt"

	"albion-market-data/collector/internal/domain"
)

type HistoryStore interface {
	Append(ctx context.Context, capture domain.CapturedHistory) error
}

type Service struct {
	store HistoryStore
}

func NewService(store HistoryStore) *Service {
	return &Service{store: store}
}

func (s *Service) CaptureHistory(ctx context.Context, capture domain.CapturedHistory) error {
	if err := capture.Validate(); err != nil {
		return fmt.Errorf("validate captured history: %w", err)
	}
	if err := s.store.Append(ctx, capture); err != nil {
		return fmt.Errorf("persist captured history: %w", err)
	}
	return nil
}

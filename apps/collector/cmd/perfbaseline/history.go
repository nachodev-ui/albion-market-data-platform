// Performance harness for fictional Albion Online in-game market data; no financial trading or real-money transactions.
package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"

	"albion-market-data/collector/internal/domain"
	"albion-market-data/collector/internal/normalization"
	"albion-market-data/collector/internal/storage/normalizedjsonl"
)

func addHistoryScenarios(target *report, root string, samples int, normalizer *normalization.Service) (domain.NormalizedHistory, error) {
	capture := makeHistoryCapture(68)
	if err := addMeasured(target, "normalize_history_68_buckets", samples, func() (sampleDetails, error) {
		history, err := normalizer.NormalizeHistory(capture)
		return sampleDetails{Counters: map[string]int64{"buckets": int64(len(history.History))}}, err
	}); err != nil {
		return domain.NormalizedHistory{}, err
	}
	history, err := normalizer.NormalizeHistory(capture)
	if err != nil {
		return domain.NormalizedHistory{}, err
	}
	if err := addMeasured(target, "serialize_history_68_buckets", samples, func() (sampleDetails, error) {
		encoded, err := json.Marshal(history)
		return sampleDetails{Artifacts: map[string]int64{"json_payload": int64(len(encoded))}}, err
	}); err != nil {
		return domain.NormalizedHistory{}, err
	}
	if err := addMeasured(target, "append_history_68_buckets", samples, func() (sampleDetails, error) {
		directory, err := os.MkdirTemp(root, "history-")
		if err != nil {
			return sampleDetails{}, err
		}
		defer os.RemoveAll(directory)
		store, err := normalizedjsonl.NewStore(directory)
		if err != nil {
			return sampleDetails{}, err
		}
		stored, err := store.AppendHistory(context.Background(), history)
		return sampleDetails{Artifacts: map[string]int64{"history_jsonl": fileSize(filepath.Join(directory, "market-history-2026-07-03.jsonl"))}, Counters: map[string]int64{"stored": boolInt(stored), "buckets": 68}}, err
	}); err != nil {
		return domain.NormalizedHistory{}, err
	}
	return history, nil
}

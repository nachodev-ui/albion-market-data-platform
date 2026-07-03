package main

import (
	"encoding/json"
	"fmt"

	"albion-market-data/collector/internal/catalog"
	"albion-market-data/collector/internal/domain"
	"albion-market-data/collector/internal/normalization"
)

func addOrderScenarios(target *report, root string, samples, size int, itemCatalog *catalog.Catalog, normalizer *normalization.Service) ([]domain.NormalizedMarketOrder, error) {
	upload := makeOrderUpload(size)
	normalized, err := normalizer.NormalizeOrders("perfbaseline", "west", baselineTime(), upload)
	if err != nil {
		return nil, err
	}
	if err := addMeasured(target, fmt.Sprintf("normalize_orders_%d", size), samples, func() (sampleDetails, error) {
		orders, err := normalizer.NormalizeOrders("perfbaseline", "west", baselineTime(), upload)
		return sampleDetails{Counters: map[string]int64{"orders": int64(len(orders))}}, err
	}); err != nil {
		return nil, err
	}
	if err := addMeasured(target, fmt.Sprintf("serialize_orders_%d", size), samples, func() (sampleDetails, error) {
		encoded, err := json.Marshal(normalized)
		return sampleDetails{Artifacts: map[string]int64{"json_payload": int64(len(encoded))}}, err
	}); err != nil {
		return nil, err
	}
	if err := addOrderPersistenceScenarios(target, root, samples, size, itemCatalog, upload, normalized); err != nil {
		return nil, err
	}
	if err := addQueueScenarios(target, root, samples, size); err != nil {
		return nil, err
	}
	return normalized, nil
}

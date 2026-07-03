package main

import (
	"albion-market-data/collector/internal/domain"
	"albion-market-data/collector/internal/normalization"
)

func runLocalScenarios(target *report, root string, samples int) error {
	itemCatalog, err := createCatalog(root)
	if err != nil {
		return err
	}
	normalizer, err := normalization.NewService(itemCatalog, discardStore{})
	if err != nil {
		return err
	}

	ordersBySize := make(map[int][]domain.NormalizedMarketOrder)
	for _, size := range []int{1000, 10000} {
		orders, err := addOrderScenarios(target, root, samples, size, itemCatalog, normalizer)
		if err != nil {
			return err
		}
		ordersBySize[size] = orders
	}

	history, err := addHistoryScenarios(target, root, samples, normalizer)
	if err != nil {
		return err
	}
	if err := addReadScenarios(target, root, samples, ordersBySize[10000], history); err != nil {
		return err
	}
	return addRecoveryScenario(target, root, makePriceEntries(1000), min(samples, 10))
}

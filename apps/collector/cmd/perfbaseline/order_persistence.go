// Performance harness for fictional Albion Online in-game market data; no financial trading or real-money transactions.
package main

import (
	"albion-market-data/collector/internal/catalog"
	"albion-market-data/collector/internal/domain"
)

func addOrderPersistenceScenarios(target *report, root string, samples, size int, itemCatalog *catalog.Catalog, upload domain.MarketOrdersUpload, normalized []domain.NormalizedMarketOrder) error {
	if err := addNormalizedFileScenario(target, root, samples, size, normalized); err != nil {
		return err
	}
	if err := addLocalDatabaseScenario(target, root, samples, size, normalized); err != nil {
		return err
	}
	return addCaptureScenario(target, root, min(samples, 10), size, itemCatalog, upload)
}

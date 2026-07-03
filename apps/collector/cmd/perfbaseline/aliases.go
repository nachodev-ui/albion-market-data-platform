// Albion Online game-data benchmark aliases.
package main

import "albion-market-data/collector/internal/domain"

type normalizedOrderBatch = []domain.NormalizedMarketOrder
type capturedOrderBatch = domain.MarketOrdersUpload

package httpapi

import (
	"net/http"
	"strings"
	"time"

	"albion-market-data/collector/internal/domain"
	"albion-market-data/collector/internal/storage/queryjsonl"
)

const priceSnapshotWindow = 2 * time.Minute

type priceRow struct {
	Server           string                   `json:"server"`
	ItemIdentifier   string                   `json:"itemIdentifier"`
	MarketKey        string                   `json:"marketKey,omitempty"`
	Location         domain.LocationDimension `json:"location"`
	Quality          uint8                    `json:"quality"`
	SellPriceMin     *int64                   `json:"sellPriceMin"`
	SellPriceMinDate *time.Time               `json:"sellPriceMinDate"`
	BuyPriceMax      *int64                   `json:"buyPriceMax"`
	BuyPriceMaxDate  *time.Time               `json:"buyPriceMaxDate"`
}

func (h *Handler) listPrices(w http.ResponseWriter, r *http.Request) {
	quality, err := parseUint8(r.URL.Query().Get("quality"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if quality == 0 {
		writeError(w, http.StatusBadRequest, "quality is required")
		return
	}
	server, err := parseServer(r.URL.Query().Get("server"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	itemIDs := parseCSV(r.URL.Query().Get("itemIds"))
	if len(itemIDs) == 0 {
		itemIDs = parseCSV(r.URL.Query().Get("itemId"))
	}
	if len(itemIDs) == 0 {
		writeError(w, http.StatusBadRequest, "itemIds is required")
		return
	}
	if len(itemIDs) > 200 {
		writeError(w, http.StatusBadRequest, "itemIds accepts at most 200 identifiers")
		return
	}
	locationID, locationName, market, err := h.resolveLocation(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if locationID == "" && locationName == "" {
		writeError(w, http.StatusBadRequest, "marketKey, location or locationId is required")
		return
	}
	now := h.now().UTC()
	rows := make([]priceRow, 0, len(itemIDs))
	for _, itemID := range itemIDs {
		orders, err := h.repository.ListOrders(r.Context(), queryjsonl.OrderFilter{Server: server, ItemID: itemID, LocationID: locationID, LocationName: locationName, Quality: quality, Limit: 5000})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "could not calculate current prices")
			return
		}
		row := priceRow{Server: server, ItemIdentifier: itemID, Location: domain.LocationDimension{ID: locationID, Name: locationName}, Quality: quality}
		if market != nil {
			row.MarketKey = market.Key
			row.Location.MarketKey = market.Key
		}
		latestCaptureBySide := map[string]time.Time{}
		for _, order := range orders {
			if order.Amount <= 0 || !order.ExpiresAt.After(now) {
				continue
			}
			if order.CapturedAt.After(latestCaptureBySide[order.Side]) {
				latestCaptureBySide[order.Side] = order.CapturedAt
			}
		}
		for _, order := range orders {
			if order.Amount <= 0 || !order.ExpiresAt.After(now) {
				continue
			}
			latestCapture := latestCaptureBySide[order.Side]
			if latestCapture.IsZero() || order.CapturedAt.Before(latestCapture.Add(-priceSnapshotWindow)) {
				continue
			}
			if row.Location.ID == "" {
				row.Location.ID = order.Location.ID
			}
			if row.Location.Name == "" {
				row.Location.Name = order.Location.Name
			}
			switch order.Side {
			case "sell":
				if row.SellPriceMin == nil || order.UnitPrice < *row.SellPriceMin {
					price, capturedAt := order.UnitPrice, order.CapturedAt
					row.SellPriceMin, row.SellPriceMinDate = &price, &capturedAt
				}
			case "buy":
				if row.BuyPriceMax == nil || order.UnitPrice > *row.BuyPriceMax {
					price, capturedAt := order.UnitPrice, order.CapturedAt
					row.BuyPriceMax, row.BuyPriceMaxDate = &price, &capturedAt
				}
			}
		}
		rows = append(rows, row)
	}
	writeJSON(w, http.StatusOK, pricesResponse{Count: len(rows), Data: rows, Source: "local-market-service", CalculatedAt: now})
}

func parseCSV(value string) []string {
	seen := make(map[string]struct{})
	result := make([]string, 0)
	for _, candidate := range strings.Split(value, ",") {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if _, exists := seen[candidate]; exists {
			continue
		}
		seen[candidate] = struct{}{}
		result = append(result, candidate)
	}
	return result
}

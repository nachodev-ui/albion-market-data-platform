package httpapi

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"albion-market-data/collector/internal/domain"
	"albion-market-data/collector/internal/storage/queryjsonl"
)

func (h *Handler) listHistory(w http.ResponseWriter, r *http.Request) {
	quality, err := parseUint8(r.URL.Query().Get("quality"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	limit, err := parseLimit(r.URL.Query().Get("limit"), 500)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	server, err := parseServer(r.URL.Query().Get("server"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	locationID, locationName, _, err := h.resolveLocation(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	records, err := h.repository.ListHistories(r.Context(), queryjsonl.HistoryFilter{
		Server:       server,
		ItemID:       strings.TrimSpace(r.URL.Query().Get("itemId")),
		LocationID:   locationID,
		LocationName: locationName,
		Quality:      quality,
		Period:       strings.TrimSpace(r.URL.Query().Get("period")),
		Limit:        limit,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not query normalized history")
		return
	}
	writeJSON(w, http.StatusOK, historyResponse{Count: len(records), Data: records, Source: "local-market-service"})
}

func (h *Handler) listOrders(w http.ResponseWriter, r *http.Request) {
	quality, err := parseUint8(r.URL.Query().Get("quality"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	limit, err := parseLimit(r.URL.Query().Get("limit"), 5000)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	server, err := parseServer(r.URL.Query().Get("server"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	locationID, locationName, _, err := h.resolveLocation(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	records, err := h.repository.ListOrders(r.Context(), queryjsonl.OrderFilter{
		Server:       server,
		ItemID:       strings.TrimSpace(r.URL.Query().Get("itemId")),
		LocationID:   locationID,
		LocationName: locationName,
		Quality:      quality,
		AuctionType:  strings.TrimSpace(r.URL.Query().Get("auctionType")),
		Side:         strings.TrimSpace(r.URL.Query().Get("side")),
		Limit:        limit,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not query normalized orders")
		return
	}
	writeJSON(w, http.StatusOK, ordersResponse{Count: len(records), Data: records, Source: "local-market-service"})
}

func (h *Handler) listMarkets(w http.ResponseWriter, r *http.Request) {
	includeDisabled := strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("includeDisabled")), "true")
	markets := h.marketCatalog.Markets(includeDisabled)
	writeJSON(w, http.StatusOK, marketsResponse{Count: len(markets), Data: markets, Source: "local-market-service"})
}

func (h *Handler) resolveLocation(r *http.Request) (string, string, *domain.MarketDefinition, error) {
	marketKey := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("marketKey")))
	if marketKey != "" {
		market, ok := h.marketCatalog.Market(marketKey)
		if !ok {
			return "", "", nil, fmt.Errorf("unknown marketKey %q", marketKey)
		}
		if !market.Enabled {
			return "", "", nil, fmt.Errorf("market %q is not enabled", marketKey)
		}
		if market.MarketLocationID == nil || strings.TrimSpace(*market.MarketLocationID) == "" {
			return "", "", nil, fmt.Errorf("market %q has no observed market location yet", marketKey)
		}
		return *market.MarketLocationID, market.Name, &market, nil
	}
	return strings.TrimSpace(r.URL.Query().Get("locationId")), strings.TrimSpace(r.URL.Query().Get("location")), nil, nil
}

func parseUint8(value string) (uint8, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, nil
	}
	parsed, err := strconv.ParseUint(value, 10, 8)
	if err != nil || parsed < 1 || parsed > 5 {
		return 0, fmt.Errorf("quality must be an integer between 1 and 5")
	}
	return uint8(parsed), nil
}

func parseLimit(value string, maximum int) (int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 100, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 1 || parsed > maximum {
		return 0, fmt.Errorf("limit must be an integer between 1 and %d", maximum)
	}
	return parsed, nil
}

func parseServer(value string) (string, error) {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return "", nil
	}
	if value != "west" && value != "east" && value != "europe" {
		return "", fmt.Errorf("server must be west, east or europe")
	}
	return value, nil
}

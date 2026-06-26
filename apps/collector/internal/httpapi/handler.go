package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"time"

	"albion-market-data/collector/internal/catalog"
	"albion-market-data/collector/internal/domain"
	"albion-market-data/collector/internal/storage/queryjsonl"
	"albion-market-data/collector/internal/upstream"
)

type Repository interface {
	ListHistories(context.Context, queryjsonl.HistoryFilter) ([]domain.NormalizedHistory, error)
	ListOrders(context.Context, queryjsonl.OrderFilter) ([]domain.NormalizedMarketOrder, error)
}

type statsProvider interface {
	Stats() queryjsonl.RepositoryStats
}

type ForwarderStatusProvider interface {
	Snapshot() upstream.ForwarderSnapshot
}

type HistoryForwarderStatusProvider interface {
	Snapshot() upstream.HistoryForwarderSnapshot
}

type StatusConfig struct {
	ServiceName                   string
	Environment                   string
	StartedAt                     time.Time
	Forwarder                     ForwarderStatusProvider
	ForwarderQueueCapacity        int
	HistoryForwarder              HistoryForwarderStatusProvider
	HistoryForwarderQueueCapacity int
}

type Handler struct {
	repository                    Repository
	marketCatalog                 *catalog.Catalog
	now                           func() time.Time
	serviceName                   string
	environment                   string
	startedAt                     time.Time
	forwarder                     ForwarderStatusProvider
	forwarderQueueCapacity        int
	historyForwarder              HistoryForwarderStatusProvider
	historyForwarderQueueCapacity int
}

func NewHandler(repository Repository, marketCatalog *catalog.Catalog, statusConfigs ...StatusConfig) (*Handler, error) {
	if repository == nil {
		return nil, fmt.Errorf("query repository is required")
	}
	if marketCatalog == nil {
		return nil, fmt.Errorf("market catalog is required")
	}
	config := StatusConfig{
		ServiceName: "albion-market-data-platform",
		Environment: "development",
		StartedAt:   time.Now().UTC(),
	}
	if len(statusConfigs) > 0 {
		provided := statusConfigs[0]
		if strings.TrimSpace(provided.ServiceName) != "" {
			config.ServiceName = strings.TrimSpace(provided.ServiceName)
		}
		if strings.TrimSpace(provided.Environment) != "" {
			config.Environment = strings.TrimSpace(provided.Environment)
		}
		if !provided.StartedAt.IsZero() {
			config.StartedAt = provided.StartedAt.UTC()
		}
		if !isNilProvider(provided.Forwarder) {
			config.Forwarder = provided.Forwarder
		}
		if !isNilProvider(provided.HistoryForwarder) {
			config.HistoryForwarder = provided.HistoryForwarder
		}
		config.ForwarderQueueCapacity = provided.ForwarderQueueCapacity
		config.HistoryForwarderQueueCapacity = provided.HistoryForwarderQueueCapacity
	}
	return &Handler{
		repository:                    repository,
		marketCatalog:                 marketCatalog,
		now:                           time.Now,
		serviceName:                   config.ServiceName,
		environment:                   config.Environment,
		startedAt:                     config.StartedAt,
		forwarder:                     config.Forwarder,
		forwarderQueueCapacity:        config.ForwarderQueueCapacity,
		historyForwarder:              config.HistoryForwarder,
		historyForwarderQueueCapacity: config.HistoryForwarderQueueCapacity,
	}, nil
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	switch r.URL.Path {
	case "/api/v1/history":
		h.listHistory(w, r)
	case "/api/v1/orders":
		h.listOrders(w, r)
	case "/api/v1/prices":
		h.listPrices(w, r)
	case "/api/v1/status":
		h.status(w)
	case "/api/v1/markets":
		h.listMarkets(w, r)
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "endpoint not found"})
	}
}

func (h *Handler) listHistory(w http.ResponseWriter, r *http.Request) {
	quality, err := parseUint8(r.URL.Query().Get("quality"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	limit, err := parseLimit(r.URL.Query().Get("limit"), 500)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	server, err := parseServer(r.URL.Query().Get("server"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	locationID, locationName, _, err := h.resolveLocation(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
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
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not query normalized history"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"count":  len(records),
		"data":   records,
		"source": "local-market-service",
	})
}

func (h *Handler) listOrders(w http.ResponseWriter, r *http.Request) {
	quality, err := parseUint8(r.URL.Query().Get("quality"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	limit, err := parseLimit(r.URL.Query().Get("limit"), 5000)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	server, err := parseServer(r.URL.Query().Get("server"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	locationID, locationName, _, err := h.resolveLocation(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
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
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not query normalized orders"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"count":  len(records),
		"data":   records,
		"source": "local-market-service",
	})
}

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
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if quality == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "quality is required"})
		return
	}
	server, err := parseServer(r.URL.Query().Get("server"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	itemIDs := parseCSV(r.URL.Query().Get("itemIds"))
	if len(itemIDs) == 0 {
		itemIDs = parseCSV(r.URL.Query().Get("itemId"))
	}
	if len(itemIDs) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "itemIds is required"})
		return
	}
	if len(itemIDs) > 200 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "itemIds accepts at most 200 identifiers"})
		return
	}

	locationID, locationName, market, err := h.resolveLocation(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if locationID == "" && locationName == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "marketKey, location or locationId is required"})
		return
	}

	now := h.now().UTC()
	rows := make([]priceRow, 0, len(itemIDs))
	for _, itemID := range itemIDs {
		orders, err := h.repository.ListOrders(r.Context(), queryjsonl.OrderFilter{
			Server:       server,
			ItemID:       itemID,
			LocationID:   locationID,
			LocationName: locationName,
			Quality:      quality,
			Limit:        5000,
		})
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not calculate current prices"})
			return
		}

		row := priceRow{
			Server:         server,
			ItemIdentifier: itemID,
			Location: domain.LocationDimension{
				ID:   locationID,
				Name: locationName,
			},
			Quality: quality,
		}
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
					price := order.UnitPrice
					capturedAt := order.CapturedAt
					row.SellPriceMin = &price
					row.SellPriceMinDate = &capturedAt
				}
			case "buy":
				if row.BuyPriceMax == nil || order.UnitPrice > *row.BuyPriceMax {
					price := order.UnitPrice
					capturedAt := order.CapturedAt
					row.BuyPriceMax = &price
					row.BuyPriceMaxDate = &capturedAt
				}
			}
		}
		rows = append(rows, row)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"count":        len(rows),
		"data":         rows,
		"source":       "local-market-service",
		"calculatedAt": now,
	})
}

func (h *Handler) listMarkets(w http.ResponseWriter, r *http.Request) {
	includeDisabled := strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("includeDisabled")), "true")
	markets := h.marketCatalog.Markets(includeDisabled)
	writeJSON(w, http.StatusOK, map[string]any{
		"count":  len(markets),
		"data":   markets,
		"source": "local-market-service",
	})
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

	locationID := strings.TrimSpace(r.URL.Query().Get("locationId"))
	locationName := strings.TrimSpace(r.URL.Query().Get("location"))
	return locationID, locationName, nil, nil
}

func isNilProvider(provider any) bool {
	if provider == nil {
		return true
	}
	value := reflect.ValueOf(provider)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

type statusResponse struct {
	Status           string                            `json:"status"`
	Service          string                            `json:"service"`
	Environment      string                            `json:"environment"`
	Source           string                            `json:"source"`
	UptimeSeconds    int64                             `json:"uptime_seconds"`
	Repository       *queryjsonl.RepositoryStats       `json:"repository,omitempty"`
	Forwarder        upstream.ForwarderSnapshot        `json:"forwarder"`
	PriceForwarder   upstream.ForwarderSnapshot        `json:"price_forwarder"`
	HistoryForwarder upstream.HistoryForwarderSnapshot `json:"history_forwarder"`
}

func (h *Handler) status(w http.ResponseWriter) {
	now := h.now().UTC()
	uptime := now.Sub(h.startedAt)
	if uptime < 0 {
		uptime = 0
	}

	priceForwarder := upstream.DisabledSnapshot(h.forwarderQueueCapacity)
	if !isNilProvider(h.forwarder) {
		priceForwarder = h.forwarder.Snapshot()
	}
	historyForwarder := upstream.DisabledHistorySnapshot(h.historyForwarderQueueCapacity)
	if !isNilProvider(h.historyForwarder) {
		historyForwarder = h.historyForwarder.Snapshot()
	}

	status := "ok"
	if forwarderIsDegraded(priceForwarder.Enabled, priceForwarder.Status) ||
		forwarderIsDegraded(historyForwarder.Enabled, historyForwarder.Status) {
		status = "degraded"
	}

	payload := statusResponse{
		Status:           status,
		Service:          h.serviceName,
		Environment:      h.environment,
		Source:           "local-market-service",
		UptimeSeconds:    int64(uptime / time.Second),
		Forwarder:        priceForwarder,
		PriceForwarder:   priceForwarder,
		HistoryForwarder: historyForwarder,
	}
	if provider, ok := h.repository.(statsProvider); ok {
		stats := provider.Stats()
		payload.Repository = &stats
	}
	writeJSON(w, http.StatusOK, payload)
}

func forwarderIsDegraded(enabled bool, status string) bool {
	return enabled && (status == "degraded" || status == "stopped")
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

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

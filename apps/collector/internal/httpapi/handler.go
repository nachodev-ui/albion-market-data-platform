package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"time"

	"albion-market-data/collector/internal/catalog"
	"albion-market-data/collector/internal/domain"
	"albion-market-data/collector/internal/storage/queryjsonl"
	"albion-market-data/collector/internal/upstream"
)

const (
	RouteHealth  = "/healthz"
	RouteReady   = "/readyz"
	RouteStatus  = "/api/v1/status"
	RouteMarkets = "/api/v1/markets"
	RoutePrices  = "/api/v1/prices"
	RouteHistory = "/api/v1/history"
	RouteOrders  = "/api/v1/orders"

	defaultMaxQueryBytes  = 16 << 10
	defaultMaxQueryValues = 256
	maxQueryKeyBytes      = 64
	maxQueryValueBytes    = 4096
)

var DocumentedGETRoutes = []string{
	RouteHealth,
	RouteReady,
	RouteStatus,
	RouteMarkets,
	RoutePrices,
	RouteHistory,
	RouteOrders,
}

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
	AllowedOrigins                []string
	MaxQueryBytes                 int
	MaxQueryValues                int
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
	allowedOrigins                map[string]struct{}
	maxQueryBytes                 int
	maxQueryValues                int
}

func NewHandler(repository Repository, marketCatalog *catalog.Catalog, statusConfigs ...StatusConfig) (*Handler, error) {
	if repository == nil {
		return nil, fmt.Errorf("query repository is required")
	}
	if marketCatalog == nil {
		return nil, fmt.Errorf("market catalog is required")
	}
	config := StatusConfig{
		ServiceName:    "albion-market-data-platform",
		Environment:    "development",
		StartedAt:      time.Now().UTC(),
		AllowedOrigins: []string{"http://127.0.0.1:5173", "http://localhost:5173"},
		MaxQueryBytes:  defaultMaxQueryBytes,
		MaxQueryValues: defaultMaxQueryValues,
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
		if provided.AllowedOrigins != nil {
			config.AllowedOrigins = append([]string(nil), provided.AllowedOrigins...)
		}
		if provided.MaxQueryBytes > 0 {
			config.MaxQueryBytes = provided.MaxQueryBytes
		}
		if provided.MaxQueryValues > 0 {
			config.MaxQueryValues = provided.MaxQueryValues
		}
	}
	allowedOrigins := make(map[string]struct{}, len(config.AllowedOrigins))
	for _, origin := range config.AllowedOrigins {
		origin = strings.TrimSpace(origin)
		if origin != "" {
			allowedOrigins[origin] = struct{}{}
		}
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
		allowedOrigins:                allowedOrigins,
		maxQueryBytes:                 config.MaxQueryBytes,
		maxQueryValues:                config.MaxQueryValues,
	}, nil
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	setSecurityHeaders(w)
	if !h.applyCORS(w, r) {
		return
	}
	if !isDocumentedRoute(r.URL.Path) {
		writeError(w, http.StatusNotFound, "endpoint not found")
		return
	}
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET, OPTIONS")
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if status, err := h.validateQuery(r.URL); err != nil {
		writeError(w, status, err.Error())
		return
	}

	switch r.URL.Path {
	case RouteHealth:
		h.health(w)
	case RouteReady:
		h.readiness(w)
	case RouteHistory:
		h.listHistory(w, r)
	case RouteOrders:
		h.listOrders(w, r)
	case RoutePrices:
		h.listPrices(w, r)
	case RouteStatus:
		h.status(w)
	case RouteMarkets:
		h.listMarkets(w, r)
	}
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

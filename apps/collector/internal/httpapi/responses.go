package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"albion-market-data/collector/internal/domain"
)

type errorResponse struct {
	Error string `json:"error"`
}

type healthResponse struct {
	Status  string `json:"status"`
	Service string `json:"service"`
}

type readinessChecks struct {
	Catalog    string `json:"catalog"`
	Repository string `json:"repository"`
}

type readinessResponse struct {
	Status  string          `json:"status"`
	Service string          `json:"service"`
	Checks  readinessChecks `json:"checks"`
}

type historyResponse struct {
	Count  int                        `json:"count"`
	Data   []domain.NormalizedHistory `json:"data"`
	Source string                     `json:"source"`
}

type ordersResponse struct {
	Count  int                            `json:"count"`
	Data   []domain.NormalizedMarketOrder `json:"data"`
	Source string                         `json:"source"`
}

type pricesResponse struct {
	Count        int        `json:"count"`
	Data         []priceRow `json:"data"`
	Source       string     `json:"source"`
	CalculatedAt time.Time  `json:"calculatedAt"`
}

type marketsResponse struct {
	Count  int                       `json:"count"`
	Data   []domain.MarketDefinition `json:"data"`
	Source string                    `json:"source"`
}

func (h *Handler) health(w http.ResponseWriter) {
	writeJSON(w, http.StatusOK, healthResponse{Status: "ok", Service: h.serviceName})
}

func (h *Handler) readiness(w http.ResponseWriter) {
	checks := readinessChecks{Catalog: "ok", Repository: "ok"}
	status := "ready"
	statusCode := http.StatusOK
	if len(h.marketCatalog.Markets(false)) == 0 {
		checks.Catalog = "unavailable"
		status = "not_ready"
		statusCode = http.StatusServiceUnavailable
	}
	if provider, ok := h.repository.(statsProvider); ok {
		stats := provider.Stats()
		if strings.TrimSpace(stats.Storage) == "" {
			checks.Repository = "unavailable"
			status = "not_ready"
			statusCode = http.StatusServiceUnavailable
		}
	}
	writeJSON(w, statusCode, readinessResponse{Status: status, Service: h.serviceName, Checks: checks})
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorResponse{Error: message})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

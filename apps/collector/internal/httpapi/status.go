package httpapi

import (
	"net/http"
	"time"

	"albion-market-data/collector/internal/storage/queryjsonl"
	"albion-market-data/collector/internal/upstream"
)

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
	if forwarderIsDegraded(priceForwarder.Enabled, priceForwarder.Status) || forwarderIsDegraded(historyForwarder.Enabled, historyForwarder.Status) {
		status = "degraded"
	}
	payload := statusResponse{Status: status, Service: h.serviceName, Environment: h.environment, Source: "local-market-service", UptimeSeconds: int64(uptime / time.Second), Forwarder: priceForwarder, PriceForwarder: priceForwarder, HistoryForwarder: historyForwarder}
	if provider, ok := h.repository.(statsProvider); ok {
		stats := provider.Stats()
		payload.Repository = &stats
	}
	writeJSON(w, http.StatusOK, payload)
}

func forwarderIsDegraded(enabled bool, status string) bool {
	return enabled && (status == "degraded" || status == "stopped")
}

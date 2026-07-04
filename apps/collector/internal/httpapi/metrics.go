package httpapi

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"albion-market-data/collector/internal/observability"
	"albion-market-data/collector/internal/upstream"
)

const RouteMetrics = "/metrics"

type MetricsConfig struct {
	Registry                      *observability.Registry
	Storage                       *observability.StorageUsage
	Build                         observability.BuildInfo
	Forwarder                     ForwarderStatusProvider
	ForwarderQueueCapacity        int
	HistoryForwarder              HistoryForwarderStatusProvider
	HistoryForwarderQueueCapacity int
}

type MetricsHandler struct {
	registry                      *observability.Registry
	storage                       *observability.StorageUsage
	build                         observability.BuildInfo
	forwarder                     ForwarderStatusProvider
	forwarderQueueCapacity        int
	historyForwarder              HistoryForwarderStatusProvider
	historyForwarderQueueCapacity int
}

func NewMetricsHandler(config MetricsConfig) *MetricsHandler {
	if config.Registry == nil {
		config.Registry = observability.NewRegistry(time.Now().UTC())
	}
	if strings.TrimSpace(config.Build.Version) == "" && strings.TrimSpace(config.Build.Commit) == "" && strings.TrimSpace(config.Build.GoVersion) == "" {
		config.Build = observability.CurrentBuildInfo()
	}
	return &MetricsHandler{
		registry:                      config.Registry,
		storage:                       config.Storage,
		build:                         config.Build,
		forwarder:                     config.Forwarder,
		forwarderQueueCapacity:        config.ForwarderQueueCapacity,
		historyForwarder:              config.HistoryForwarder,
		historyForwarderQueueCapacity: config.HistoryForwarderQueueCapacity,
	}
}

func (h *MetricsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	setSecurityHeaders(w)
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(h.render()))
}

func (h *MetricsHandler) render() string {
	var output strings.Builder
	registry := h.registry.Snapshot()
	storage := observability.StorageUsageSnapshot{}
	if h.storage != nil {
		storage = h.storage.Snapshot()
	}

	price := upstream.DisabledSnapshot(h.forwarderQueueCapacity)
	if !isNilProvider(h.forwarder) {
		price = h.forwarder.Snapshot()
	}
	history := upstream.DisabledHistorySnapshot(h.historyForwarderQueueCapacity)
	if !isNilProvider(h.historyForwarder) {
		history = h.historyForwarder.Snapshot()
	}

	writeMetricHeader(&output, "albion_receiver_uptime_seconds", "Receiver process uptime in seconds.", "gauge")
	writeMetric(&output, "albion_receiver_uptime_seconds", nil, strconv.FormatInt(registry.UptimeSeconds, 10))

	writeMetricHeader(&output, "albion_receiver_build_info", "Receiver build metadata.", "gauge")
	writeMetric(&output, "albion_receiver_build_info", map[string]string{
		"version":    defaultMetricLabel(h.build.Version, "dev"),
		"commit":     defaultMetricLabel(h.build.Commit, "unknown"),
		"built_at":   h.build.BuiltAt,
		"go_version": h.build.GoVersion,
		"modified":   strconv.FormatBool(h.build.Modified),
	}, "1")

	writeCounterMap(&output, "albion_receiver_captures_received_total", "Market capture HTTP payloads received by topic.", "topic", registry.CapturesReceived)
	writeUint64Map(&output, "albion_receiver_capture_bytes_received_total", "Raw bytes received in market capture payloads by topic.", "topic", registry.CaptureBytes)
	writeCounterMap(&output, "albion_receiver_entries_received_total", "Market entries received by pipeline.", "pipeline", registry.EntriesReceived)
	writeCounterMap(&output, "albion_receiver_entries_stored_total", "Market entries stored by pipeline.", "pipeline", registry.EntriesStored)
	writeCounterMap(&output, "albion_receiver_duplicates_total", "Duplicate market entries ignored by pipeline.", "pipeline", registry.Duplicates)
	writeCounterMap(&output, "albion_receiver_normalization_errors_total", "Payload decode or normalization errors by pipeline.", "pipeline", registry.NormalizationErrors)
	writeLastTimestampMap(&output, "albion_receiver_last_capture_timestamp_seconds", "Unix timestamp of the last capture received by topic.", "topic", registry.CapturesReceived)
	writeLastTimestampMap(&output, "albion_receiver_last_entry_received_timestamp_seconds", "Unix timestamp of the last market entry received by pipeline.", "pipeline", registry.EntriesReceived)
	writeLastTimestampMap(&output, "albion_receiver_last_entry_stored_timestamp_seconds", "Unix timestamp of the last market entry stored by pipeline.", "pipeline", registry.EntriesStored)

	writeMetricHeader(&output, "albion_receiver_storage_writes_total", "Successful durable storage writes by area.", "counter")
	writeMetricHeader(&output, "albion_receiver_storage_errors_total", "Durable storage errors by area.", "counter")
	writeMetricHeader(&output, "albion_receiver_storage_last_success_timestamp_seconds", "Unix timestamp of the last successful storage write by area.", "gauge")
	writeMetricHeader(&output, "albion_receiver_storage_last_error_timestamp_seconds", "Unix timestamp of the last storage error by area.", "gauge")
	for _, area := range sortedStorageKeys(registry.Storage) {
		snapshot := registry.Storage[area]
		labels := map[string]string{"area": area}
		writeMetric(&output, "albion_receiver_storage_writes_total", labels, strconv.FormatUint(snapshot.WritesTotal, 10))
		writeMetric(&output, "albion_receiver_storage_errors_total", labels, strconv.FormatUint(snapshot.ErrorsTotal, 10))
		writeTimestampMetric(&output, "albion_receiver_storage_last_success_timestamp_seconds", labels, snapshot.LastWriteAt)
		writeTimestampMetric(&output, "albion_receiver_storage_last_error_timestamp_seconds", labels, snapshot.LastErrorAt)
	}

	writeMetricHeader(&output, "albion_receiver_storage_bytes", "Current bytes used by receiver storage area.", "gauge")
	writeMetric(&output, "albion_receiver_storage_bytes", map[string]string{"area": "raw"}, strconv.FormatInt(storage.RawBytes, 10))
	writeMetric(&output, "albion_receiver_storage_bytes", map[string]string{"area": "normalized"}, strconv.FormatInt(storage.NormalizedBytes, 10))
	writeMetric(&output, "albion_receiver_storage_bytes", map[string]string{"area": "database"}, strconv.FormatInt(storage.DatabaseBytes, 10))
	writeMetric(&output, "albion_receiver_storage_bytes", map[string]string{"area": "outbox"}, strconv.FormatInt(storage.OutboxBytes, 10))
	writeMetric(&output, "albion_receiver_storage_bytes", map[string]string{"area": "total"}, strconv.FormatInt(storage.TotalBytes, 10))
	writeMetricHeader(&output, "albion_receiver_storage_max_bytes", "Configured maximum bytes for receiver data storage.", "gauge")
	writeMetric(&output, "albion_receiver_storage_max_bytes", nil, strconv.FormatInt(storage.MaxBytes, 10))
	writeMetricHeader(&output, "albion_receiver_storage_measurement_success", "Whether the latest storage size measurement succeeded by area.", "gauge")
	for _, area := range []string{"raw", "normalized", "database", "outbox"} {
		value := "1"
		if _, failed := storage.Errors[area]; failed {
			value = "0"
		}
		writeMetric(&output, "albion_receiver_storage_measurement_success", map[string]string{"area": area}, value)
	}

	writeForwarderMetrics(&output, "prices", forwarderViewFromPrice(price))
	writeForwarderMetrics(&output, "history", forwarderViewFromHistory(history))
	return output.String()
}

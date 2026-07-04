package httpingest

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"runtime"
	"strings"
	"time"

	"albion-market-data/collector/internal/domain"
	"albion-market-data/collector/internal/normalization"
	"albion-market-data/collector/internal/observability"
	"albion-market-data/collector/internal/upstream"
)

const maxRequestBytes int64 = 10 << 20

type RawStore interface {
	AppendRaw(ctx context.Context, event domain.RawIngestEvent) error
}

type PriceForwarder interface {
	Enqueue(entry upstream.PriceIngest) bool
}

type PriceBatchForwarder interface {
	EnqueueBatch(entries []upstream.PriceIngest) (accepted int, dropped int)
}

type HistoryForwarder interface {
	Enqueue(entry upstream.HistoryIngest) bool
}

type eventLogger interface {
	Printf(format string, args ...any)
}

type Options struct {
	MaxConcurrent int
	Metrics       *observability.Registry
}

type Handler struct {
	server           string
	rawStore         RawStore
	normalizer       *normalization.Service
	priceForwarder   PriceForwarder
	historyForwarder HistoryForwarder
	logger           eventLogger
	metrics          *observability.Registry
	now              func() time.Time
	ingestSlots      chan struct{}
}

func NewHandler(server string, rawStore RawStore, normalizer *normalization.Service, priceForwarder PriceForwarder, historyForwarder HistoryForwarder, logger eventLogger) (*Handler, error) {
	return NewHandlerWithOptions(server, rawStore, normalizer, priceForwarder, historyForwarder, logger, Options{})
}

func NewHandlerWithOptions(server string, rawStore RawStore, normalizer *normalization.Service, priceForwarder PriceForwarder, historyForwarder HistoryForwarder, logger eventLogger, options Options) (*Handler, error) {
	if server != "west" && server != "east" && server != "europe" {
		return nil, fmt.Errorf("unsupported server %q", server)
	}
	if rawStore == nil {
		return nil, fmt.Errorf("raw store is required")
	}
	if normalizer == nil {
		return nil, fmt.Errorf("normalization service is required")
	}
	if isNilInterface(priceForwarder) {
		priceForwarder = nil
	}
	if isNilInterface(historyForwarder) {
		historyForwarder = nil
	}
	if logger == nil {
		logger = observability.NewLogger(nil, "auto")
	}
	maxConcurrent := options.MaxConcurrent
	if maxConcurrent <= 0 {
		maxConcurrent = runtime.GOMAXPROCS(0)
		if maxConcurrent < 4 {
			maxConcurrent = 4
		}
	}
	return &Handler{
		server:           server,
		rawStore:         rawStore,
		normalizer:       normalizer,
		priceForwarder:   priceForwarder,
		historyForwarder: historyForwarder,
		logger:           logger,
		metrics:          options.Metrics,
		now:              time.Now,
		ingestSlots:      make(chan struct{}, maxConcurrent),
	}, nil
}

func isNilInterface(value any) bool {
	if value == nil {
		return true
	}

	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/healthz" {
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}

	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	select {
	case h.ingestSlots <- struct{}{}:
		defer func() { <-h.ingestSlots }()
	default:
		w.Header().Set("Retry-After", "1")
		h.logEvent(observability.LevelRetry, "ingest.backpressure_rejected", observability.F("path", r.URL.Path), observability.F("max_concurrent", cap(h.ingestSlots)))
		writeJSON(w, http.StatusTooManyRequests, map[string]any{
			"status":     "busy",
			"retryAfter": 1,
			"error":      "receiver ingest capacity is temporarily exhausted",
		})
		return
	}

	topic := strings.Trim(r.URL.Path, "/")
	if topic == "" {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "missing ingest topic"})
		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxRequestBytes))
	if err != nil {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "request body is too large or unreadable"})
		return
	}
	h.metrics.RecordCapture(topic, len(body))
	if !json.Valid(body) {
		if pipeline := ingestPipeline(topic); pipeline != "" {
			h.metrics.RecordNormalizationError(pipeline)
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "body must be valid JSON"})
		return
	}

	receivedAt := h.now().UTC()
	rawEvent := domain.RawIngestEvent{
		SchemaVersion: 1,
		Source:        "aodp-http-ingest",
		Server:        h.server,
		Topic:         topic,
		ReceivedAt:    receivedAt,
		Payload:       append(json.RawMessage(nil), body...),
	}
	if err := h.rawStore.AppendRaw(r.Context(), rawEvent); err != nil {
		h.logEvent(observability.LevelError, "ingest.raw_failed", observability.F("topic", topic), observability.F("error", err))
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not persist raw event"})
		return
	}

	switch topic {
	case "markethistories.ingest":
		h.handleHistory(w, r, body, receivedAt)
	case "marketorders.ingest":
		h.handleOrders(w, r, body, receivedAt)
	default:
		h.logEvent(observability.LevelInfo, "ingest.raw_stored", observability.F("topic", topic), observability.F("bytes", len(body)))
		writeJSON(w, http.StatusOK, map[string]string{"status": "raw-stored", "topic": topic})
	}
}

func (h *Handler) handleHistory(w http.ResponseWriter, r *http.Request, body []byte, receivedAt time.Time) {
	var upload domain.MarketHistoriesUpload
	if err := json.Unmarshal(body, &upload); err != nil {
		h.metrics.RecordNormalizationError("history")
		h.logEvent(observability.LevelWarn, "ingest.history_invalid", observability.F("error", err))
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid market history payload"})
		return
	}

	h.metrics.RecordEntriesReceived("history", 1)

	capture := domain.CapturedHistory{
		SchemaVersion: 1,
		Source:        "aodp-http-ingest",
		Server:        h.server,
		CapturedAt:    receivedAt,
		Payload:       upload,
	}
	normalized, stored, err := h.normalizer.CaptureHistory(r.Context(), capture)
	if err != nil {
		h.metrics.RecordNormalizationError("history")
		// El evento crudo ya está a salvo. Un 202 evita que una carencia temporal
		// del catálogo haga que el cliente reintente indefinidamente.
		h.logEvent(observability.LevelWarn, "ingest.history_pending", observability.F("albion_id", upload.AlbionID), observability.F("error", err))
		writeJSON(w, http.StatusAccepted, map[string]any{
			"status":   "raw-stored-normalization-pending",
			"topic":    "markethistories.ingest",
			"albionId": upload.AlbionID,
			"error":    err.Error(),
		})
		return
	}

	forwarded := 0
	dropped := 0
	forwardedBuckets := 0
	if h.historyForwarder != nil {
		entry, buildErr := buildUpstreamHistoryEntry(normalized)
		if buildErr != nil {
			h.logEvent(
				observability.LevelWarn,
				"upstream.history_snapshot_skipped",
				observability.F("item_key", normalized.Item.ID),
				observability.F("error", buildErr),
			)
		} else if h.historyForwarder.Enqueue(entry) {
			forwarded = 1
			forwardedBuckets = len(entry.History)
		} else {
			dropped = 1
			h.logEvent(
				observability.LevelDrop,
				"upstream.history_queue_batch_drop",
				observability.F("item_key", normalized.Item.ID),
				observability.F("buckets", len(entry.History)),
			)
		}
	}

	if stored {
		h.metrics.RecordPersistence("history", 1, 0)
	} else {
		h.metrics.RecordPersistence("history", 0, 1)
	}

	h.logEvent(
		observability.LevelOK,
		"ingest.history_completed",
		observability.F("item_key", normalized.Item.ID),
		observability.F("albion_id", normalized.Item.AlbionID),
		observability.F("location_id", normalized.Location.ID),
		observability.F("quality", normalized.Quality.ID),
		observability.F("period", normalized.Period),
		observability.F("buckets", len(normalized.History)),
		observability.F("active_buckets", normalized.Summary.ActiveBuckets),
		observability.F("sold_units", normalized.Summary.SoldUnits),
		observability.F("stored", stored),
		observability.F("forwarded", forwarded),
		observability.F("forwarded_buckets", forwardedBuckets),
		observability.F("dropped", dropped),
	)
	writeJSON(w, http.StatusOK, map[string]any{
		"status":           "normalized",
		"topic":            "markethistories.ingest",
		"itemId":           normalized.Item.ID,
		"albionId":         normalized.Item.AlbionID,
		"period":           normalized.Period,
		"soldUnits":        normalized.Summary.SoldUnits,
		"stored":           stored,
		"duplicate":        !stored,
		"forwarded":        forwarded,
		"forwardedBuckets": forwardedBuckets,
		"dropped":          dropped,
		"dedupeKey":        normalized.DedupeKey,
		"capturedAt":       normalized.CapturedAt,
	})
}

func (h *Handler) handleOrders(w http.ResponseWriter, r *http.Request, body []byte, receivedAt time.Time) {
	var upload domain.MarketOrdersUpload
	if err := json.Unmarshal(body, &upload); err != nil {
		h.metrics.RecordNormalizationError("prices")
		h.logEvent(observability.LevelWarn, "ingest.orders_invalid", observability.F("error", err))
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid market orders payload"})
		return
	}
	h.metrics.RecordEntriesReceived("prices", len(upload.Orders))
	normalizedOrders, result, err := h.normalizer.CaptureOrdersDetailed(r.Context(), "aodp-http-ingest", h.server, receivedAt, upload)
	if err != nil {
		h.metrics.RecordNormalizationError("prices")
		h.logEvent(observability.LevelWarn, "ingest.orders_pending", observability.F("error", err))
		writeJSON(w, http.StatusAccepted, map[string]any{
			"status": "raw-stored-normalization-pending",
			"topic":  "marketorders.ingest",
			"error":  err.Error(),
		})
		return
	}
	forwarded := 0
	dropped := 0
	if h.priceForwarder != nil {
		entries, buildErr := buildUpstreamPriceEntries(normalizedOrders)
		if buildErr != nil {
			h.logEvent(observability.LevelWarn, "upstream.snapshot_skipped", observability.F("error", buildErr))
		} else {
			if batchForwarder, ok := h.priceForwarder.(PriceBatchForwarder); ok {
				forwarded, dropped = batchForwarder.EnqueueBatch(entries)
			} else {
				for _, entry := range entries {
					if h.priceForwarder.Enqueue(entry) {
						forwarded++
					} else {
						dropped++
					}
				}
			}
			if dropped > 0 {
				h.logEvent(observability.LevelDrop, "upstream.queue_batch_drop", observability.F("forwarded", forwarded), observability.F("dropped", dropped))
			}
		}
	}

	h.metrics.RecordPersistence("prices", result.Stored, result.Duplicates)
	h.logEvent(observability.LevelOK, "ingest.orders_completed", observability.F("received", result.Received), observability.F("stored", result.Stored), observability.F("duplicates", result.Duplicates), observability.F("forwarded", forwarded), observability.F("dropped", dropped))
	writeJSON(w, http.StatusOK, map[string]any{
		"status":     "normalized",
		"topic":      "marketorders.ingest",
		"received":   result.Received,
		"stored":     result.Stored,
		"duplicates": result.Duplicates,
		"forwarded":  forwarded,
		"dropped":    dropped,
	})
}

func ingestPipeline(topic string) string {
	switch topic {
	case "marketorders.ingest":
		return "prices"
	case "markethistories.ingest":
		return "history"
	default:
		return ""
	}
}

func (h *Handler) logEvent(level observability.Level, event string, fields ...observability.Field) {
	if logger, ok := h.logger.(interface {
		Event(observability.Level, string, ...observability.Field)
	}); ok {
		logger.Event(level, event, fields...)
		return
	}
	h.logger.Printf("%s", event)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

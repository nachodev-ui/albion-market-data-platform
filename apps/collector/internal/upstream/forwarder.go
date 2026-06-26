package upstream

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"albion-market-data/collector/internal/observability"
)

type priceSender interface {
	SendPrices(context.Context, IngestPricesRequest) (SendResult, error)
}

type Forwarder struct {
	client              priceSender
	logger              *observability.Logger
	metrics             *Metrics
	outbox              *Outbox
	server              string
	capacity            int
	batchSize           int
	flushInterval       time.Duration
	retryCount          int
	retryDelay          time.Duration
	maxDeliveryAttempts int
	maxRetryDelay       time.Duration

	wake chan struct{}
	stop chan struct{}

	stateMu             sync.RWMutex
	closed              bool
	startOnce           sync.Once
	stopOnce            sync.Once
	waiter              sync.WaitGroup
	queueDropLogCounter atomic.Uint64
	temporaryOutboxPath string
}

func NewForwarder(
	client priceSender,
	logger *observability.Logger,
	server string,
	queueSize int,
	batchSize int,
	flushInterval time.Duration,
	retryCount int,
	retryDelay time.Duration,
) (*Forwarder, error) {
	path := filepath.Join(os.TempDir(), "albion-market-data-outbox-"+newRequestID()+".json")
	outbox, err := NewOutbox(path)
	if err != nil {
		return nil, err
	}
	forwarder, err := NewForwarderWithOutbox(client, logger, server, outbox, queueSize, batchSize, flushInterval, retryCount, retryDelay, retryCount, time.Minute)
	if err != nil {
		return nil, err
	}
	forwarder.temporaryOutboxPath = path
	return forwarder, nil
}

func NewForwarderWithOutbox(
	client priceSender,
	logger *observability.Logger,
	server string,
	outbox *Outbox,
	capacity int,
	batchSize int,
	flushInterval time.Duration,
	retryCount int,
	retryDelay time.Duration,
	maxDeliveryAttempts int,
	maxRetryDelay time.Duration,
) (*Forwarder, error) {
	if client == nil {
		return nil, fmt.Errorf("upstream client is required")
	}
	if outbox == nil {
		return nil, fmt.Errorf("persistent outbox is required")
	}
	if logger == nil {
		logger = observability.NewLogger(os.Stdout, "auto")
	}
	if server != "west" && server != "east" && server != "europe" {
		return nil, fmt.Errorf("unsupported server %q", server)
	}
	if capacity <= 0 {
		capacity = 5000
	}
	if batchSize <= 0 {
		batchSize = 500
	}
	if flushInterval <= 0 {
		flushInterval = 250 * time.Millisecond
	}
	if retryCount < 1 {
		retryCount = 1
	}
	if retryDelay <= 0 {
		retryDelay = 500 * time.Millisecond
	}
	if maxDeliveryAttempts < retryCount {
		maxDeliveryAttempts = retryCount
	}
	if maxRetryDelay <= 0 {
		maxRetryDelay = 5 * time.Minute
	}

	return &Forwarder{
		client:              client,
		logger:              logger,
		metrics:             NewMetrics(),
		outbox:              outbox,
		server:              server,
		capacity:            capacity,
		batchSize:           batchSize,
		flushInterval:       flushInterval,
		retryCount:          retryCount,
		retryDelay:          retryDelay,
		maxDeliveryAttempts: maxDeliveryAttempts,
		maxRetryDelay:       maxRetryDelay,
		wake:                make(chan struct{}, 1),
		stop:                make(chan struct{}),
	}, nil
}

func (f *Forwarder) Start(ctx context.Context) {
	f.startOnce.Do(func() {
		f.metrics.SetRunning(true)
		f.logger.Event(
			observability.LevelInfo,
			"upstream.forwarder_started",
			observability.F("server", f.server),
			observability.F("batch_size", f.batchSize),
			observability.F("flush_interval", f.flushInterval),
			observability.F("outbox_capacity", f.capacity),
			observability.F("outbox_path", f.outbox.Path()),
			observability.F("attempts_per_cycle", f.retryCount),
			observability.F("max_delivery_attempts", f.maxDeliveryAttempts),
		)
		f.waiter.Add(1)
		go func() {
			defer f.waiter.Done()
			defer f.metrics.SetRunning(false)
			f.run(ctx)
		}()
		f.signal()
	})
}

func (f *Forwarder) Stop() {
	f.stopOnce.Do(func() {
		f.stateMu.Lock()
		f.closed = true
		close(f.stop)
		f.stateMu.Unlock()
	})
	f.waiter.Wait()
	f.metrics.SetRunning(false)
	f.logger.Event(
		observability.LevelInfo,
		"upstream.forwarder_stopped",
		observability.F("outbox_depth", f.outbox.Depth(PipelinePrices)),
		observability.F("outbox_capacity", f.capacity),
	)
	if f.temporaryOutboxPath != "" {
		_ = os.Remove(f.temporaryOutboxPath)
		_ = os.Remove(f.temporaryOutboxPath + ".bak")
		_ = os.Remove(f.temporaryOutboxPath + ".tmp")
	}
}

func (f *Forwarder) Enqueue(entry PriceIngest) bool {
	accepted, _ := f.EnqueueBatch([]PriceIngest{entry})
	return accepted == 1
}

func (f *Forwarder) EnqueueBatch(entries []PriceIngest) (accepted int, dropped int) {
	if len(entries) == 0 {
		return 0, 0
	}
	f.stateMu.RLock()
	closed := f.closed
	f.stateMu.RUnlock()
	if closed {
		depth := f.outbox.Depth(PipelinePrices)
		f.metrics.RecordQueueDropMany(len(entries), depth)
		f.logQueueDrop(entries[0], "forwarder_stopped", nil)
		return 0, len(entries)
	}

	accepted, depth, err := f.outbox.EnqueuePrices(f.server, entries, f.capacity)
	dropped = len(entries) - accepted
	if accepted > 0 {
		f.metrics.RecordEnqueuedMany(accepted, depth)
	}
	if dropped > 0 {
		f.metrics.RecordQueueDropMany(dropped, depth)
		reason := "outbox_full"
		if err != nil && !errors.Is(err, ErrOutboxFull) {
			reason = "outbox_write_failed"
		}
		f.logQueueDrop(entries[accepted], reason, err)
	}
	if accepted == 0 && err != nil && dropped == 0 {
		f.metrics.RecordQueueDropMany(len(entries), depth)
		f.logQueueDrop(entries[0], "outbox_write_failed", err)
		return 0, len(entries)
	}
	if depth >= f.batchSize {
		f.signal()
	}
	return accepted, dropped
}

func (f *Forwarder) Snapshot() ForwarderSnapshot {
	depth := f.outbox.Depth(PipelinePrices)
	snapshot := f.metrics.Snapshot(depth, f.capacity)
	snapshot.Outbox = f.outbox.Snapshot(PipelinePrices)
	if snapshot.Outbox.DeadLetterBatches > 0 {
		snapshot.Status = "degraded"
	}
	return snapshot
}

func (f *Forwarder) signal() {
	select {
	case f.wake <- struct{}{}:
	default:
	}
}

func (f *Forwarder) logQueueDrop(entry PriceIngest, reason string, err error) {
	droppedTotal := f.queueDropLogCounter.Add(1)
	if droppedTotal != 1 && droppedTotal%100 != 0 {
		return
	}
	fields := []observability.Field{
		observability.F("reason", reason),
		observability.F("dropped_total", droppedTotal),
		observability.F("item_key", entry.ItemKey),
		observability.F("location_id", entry.LocationID),
		observability.F("quality", entry.Quality),
		observability.F("outbox_depth", f.outbox.Depth(PipelinePrices)),
		observability.F("outbox_capacity", f.capacity),
	}
	if err != nil {
		fields = append(fields, observability.F("error", err))
	}
	f.logger.Event(observability.LevelDrop, "upstream.queue_drop", fields...)
}

func (f *Forwarder) run(ctx context.Context) {
	ticker := time.NewTicker(f.flushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-f.stop:
			f.processAvailable(context.Background())
			return
		case <-f.wake:
			f.processAvailable(ctx)
		case <-ticker.C:
			f.processAvailable(ctx)
		}
	}
}

func (f *Forwarder) processAvailable(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}
		batch, err := f.outbox.ClaimPriceBatch(f.server, f.batchSize)
		if err != nil {
			f.logger.Event(observability.LevelError, "upstream.outbox_claim_failed", observability.F("pipeline", PipelinePrices), observability.F("error", err))
			return
		}
		if batch == nil {
			return
		}
		f.processBatch(ctx, batch)
	}
}

func (f *Forwarder) processBatch(ctx context.Context, batch *ClaimedBatch) {
	payload := IngestPricesRequest{
		RequestID: batch.RequestID,
		Server:    batch.Server,
		Entries:   batch.PriceEntries,
	}
	startedAt := time.Now()
	f.metrics.BeginBatch()
	result, attemptsUsed, lastErr, permanent := f.sendCycle(ctx, payload)
	duration := time.Since(startedAt)

	if lastErr == nil {
		if err := f.outbox.Complete(batch.RequestID); err != nil {
			f.metrics.RecordDeferred(duration, result.StatusCode, err)
			f.logger.Event(observability.LevelError, "upstream.outbox_complete_failed", observability.F("request_id", batch.RequestID), observability.F("error", err))
			return
		}
		f.metrics.RecordSuccess(len(payload.Entries), duration, attemptsUsed, result.StatusCode)
		event := "upstream.batch_sent"
		if attemptsUsed > 1 || batch.Attempts > 0 || result.Response.Duplicate {
			event = "upstream.batch_recovered"
		}
		f.logger.Event(
			observability.LevelOK,
			event,
			observability.F("request_id", payload.RequestID),
			observability.F("server", payload.Server),
			observability.F("entries", len(payload.Entries)),
			observability.F("accepted", result.Response.Accepted),
			observability.F("current_rows_touched", result.Response.CurrentRowsTouched),
			observability.F("duplicate", result.Response.Duplicate),
			observability.F("http_status", result.StatusCode),
			observability.F("attempts_this_cycle", attemptsUsed),
			observability.F("attempts_before_cycle", batch.Attempts),
			observability.F("duration_ms", milliseconds(duration)),
			observability.F("outbox_depth", f.outbox.Depth(PipelinePrices)),
		)
		return
	}

	statusCode, _ := SendErrorDetails(lastErr)
	totalAttempts := batch.Attempts + attemptsUsed
	if permanent || totalAttempts >= f.maxDeliveryAttempts {
		if err := f.outbox.DeadLetter(batch.RequestID, attemptsUsed, statusCode, lastErr.Error()); err != nil {
			f.logger.Event(observability.LevelError, "upstream.outbox_dead_letter_failed", observability.F("request_id", batch.RequestID), observability.F("error", err))
		}
		f.metrics.RecordDeadLetter(len(payload.Entries), duration, statusCode, lastErr)
		f.logger.Event(
			observability.LevelDrop,
			"upstream.batch_dead_lettered",
			observability.F("request_id", payload.RequestID),
			observability.F("entries", len(payload.Entries)),
			observability.F("attempts_total", totalAttempts),
			observability.F("permanent", permanent),
			observability.F("http_status", statusCode),
			observability.F("error", lastErr),
		)
		return
	}

	delay := retryBackoff(f.retryDelay, f.maxRetryDelay, totalAttempts)
	nextAttempt := time.Now().UTC().Add(delay)
	if err := f.outbox.Reschedule(batch.RequestID, attemptsUsed, statusCode, lastErr.Error(), nextAttempt); err != nil {
		f.logger.Event(observability.LevelError, "upstream.outbox_reschedule_failed", observability.F("request_id", batch.RequestID), observability.F("error", err))
	}
	f.metrics.RecordDeferred(duration, statusCode, lastErr)
	f.logger.Event(
		observability.LevelRetry,
		"upstream.batch_persisted_for_retry",
		observability.F("request_id", payload.RequestID),
		observability.F("entries", len(payload.Entries)),
		observability.F("attempts_total", totalAttempts),
		observability.F("retry_at", nextAttempt),
		observability.F("retry_in_ms", milliseconds(delay)),
		observability.F("http_status", statusCode),
		observability.F("error", lastErr),
	)
}

func (f *Forwarder) sendCycle(ctx context.Context, payload IngestPricesRequest) (SendResult, int, error, bool) {
	var lastErr error
	var lastResult SendResult
	for attempt := 1; attempt <= f.retryCount; attempt++ {
		if ctx.Err() != nil {
			return lastResult, attempt - 1, ctx.Err(), false
		}
		attemptStartedAt := time.Now()
		result, err := f.client.SendPrices(ctx, payload)
		attemptDuration := time.Since(attemptStartedAt)
		statusCode := result.StatusCode
		if err != nil {
			if errorStatus, errorDuration := SendErrorDetails(err); errorStatus != 0 || errorDuration > 0 {
				statusCode = errorStatus
				if errorDuration > 0 {
					attemptDuration = errorDuration
				}
			}
		}
		f.metrics.RecordAttempt(attemptDuration, statusCode, err)
		if err == nil {
			return result, attempt, nil, false
		}
		lastErr = err
		lastResult = result
		if !isRetriableSendError(err) {
			return lastResult, attempt, lastErr, true
		}
		if attempt == f.retryCount {
			break
		}
		delay := time.Duration(attempt) * f.retryDelay
		f.metrics.RecordRetry()
		f.logger.Event(
			observability.LevelRetry,
			"upstream.retry_scheduled",
			observability.F("request_id", payload.RequestID),
			observability.F("attempt", attempt),
			observability.F("http_status", statusCode),
			observability.F("attempt_duration_ms", milliseconds(attemptDuration)),
			observability.F("retry_in_ms", milliseconds(delay)),
			observability.F("error", err),
		)
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return lastResult, attempt, ctx.Err(), false
		case <-timer.C:
		}
	}
	return lastResult, f.retryCount, lastErr, false
}

func isRetriableSendError(err error) bool {
	if err == nil {
		return false
	}
	statusCode, _ := SendErrorDetails(err)
	if statusCode == 0 {
		return true
	}
	if statusCode == 408 || statusCode == 425 || statusCode == 429 {
		return true
	}
	return statusCode >= 500
}

func retryBackoff(base, maximum time.Duration, attempts int) time.Duration {
	if base <= 0 {
		base = 500 * time.Millisecond
	}
	if maximum <= 0 {
		maximum = 5 * time.Minute
	}
	if attempts < 1 {
		attempts = 1
	}
	delay := base
	for index := 1; index < attempts && delay < maximum; index++ {
		if delay > maximum/2 {
			delay = maximum
			break
		}
		delay *= 2
	}
	if delay > maximum {
		return maximum
	}
	return delay
}

func milliseconds(value time.Duration) float64 {
	return float64(value) / float64(time.Millisecond)
}

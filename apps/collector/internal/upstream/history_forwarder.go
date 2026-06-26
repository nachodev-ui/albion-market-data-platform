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

const defaultHistoryMaxBatchBuckets = 100000

type historySender interface {
	SendHistory(context.Context, IngestHistoryRequest) (HistorySendResult, error)
}

type HistoryForwarder struct {
	client              historySender
	logger              *observability.Logger
	metrics             *HistoryMetrics
	outbox              *Outbox
	server              string
	capacity            int
	batchSize           int
	maxBatchBuckets     int
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

func NewHistoryForwarder(
	client historySender,
	logger *observability.Logger,
	server string,
	queueSize int,
	batchSize int,
	maxBatchBuckets int,
	flushInterval time.Duration,
	retryCount int,
	retryDelay time.Duration,
) (*HistoryForwarder, error) {
	path := filepath.Join(os.TempDir(), "albion-market-data-history-outbox-"+newRequestID()+".json")
	outbox, err := NewOutbox(path)
	if err != nil {
		return nil, err
	}
	forwarder, err := NewHistoryForwarderWithOutbox(client, logger, server, outbox, queueSize, batchSize, maxBatchBuckets, flushInterval, retryCount, retryDelay, retryCount, time.Minute)
	if err != nil {
		return nil, err
	}
	forwarder.temporaryOutboxPath = path
	return forwarder, nil
}

func NewHistoryForwarderWithOutbox(
	client historySender,
	logger *observability.Logger,
	server string,
	outbox *Outbox,
	capacity int,
	batchSize int,
	maxBatchBuckets int,
	flushInterval time.Duration,
	retryCount int,
	retryDelay time.Duration,
	maxDeliveryAttempts int,
	maxRetryDelay time.Duration,
) (*HistoryForwarder, error) {
	if client == nil {
		return nil, fmt.Errorf("upstream history client is required")
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
		capacity = 1000
	}
	if batchSize <= 0 {
		batchSize = 100
	}
	if maxBatchBuckets <= 0 || maxBatchBuckets > defaultHistoryMaxBatchBuckets {
		maxBatchBuckets = defaultHistoryMaxBatchBuckets
	}
	if flushInterval <= 0 {
		flushInterval = 500 * time.Millisecond
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

	return &HistoryForwarder{
		client:              client,
		logger:              logger,
		metrics:             NewHistoryMetrics(),
		outbox:              outbox,
		server:              server,
		capacity:            capacity,
		batchSize:           batchSize,
		maxBatchBuckets:     maxBatchBuckets,
		flushInterval:       flushInterval,
		retryCount:          retryCount,
		retryDelay:          retryDelay,
		maxDeliveryAttempts: maxDeliveryAttempts,
		maxRetryDelay:       maxRetryDelay,
		wake:                make(chan struct{}, 1),
		stop:                make(chan struct{}),
	}, nil
}

func (f *HistoryForwarder) Start(ctx context.Context) {
	f.startOnce.Do(func() {
		f.metrics.SetRunning(true)
		f.logger.Event(
			observability.LevelInfo,
			"upstream.history_forwarder_started",
			observability.F("server", f.server),
			observability.F("batch_size", f.batchSize),
			observability.F("max_batch_buckets", f.maxBatchBuckets),
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

func (f *HistoryForwarder) Stop() {
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
		"upstream.history_forwarder_stopped",
		observability.F("outbox_depth", f.outbox.Depth(PipelineHistory)),
		observability.F("outbox_capacity", f.capacity),
	)
	if f.temporaryOutboxPath != "" {
		_ = os.Remove(f.temporaryOutboxPath)
		_ = os.Remove(f.temporaryOutboxPath + ".bak")
		_ = os.Remove(f.temporaryOutboxPath + ".tmp")
	}
}

func (f *HistoryForwarder) Enqueue(entry HistoryIngest) bool {
	buckets := len(entry.History)
	if buckets == 0 || buckets > f.maxBatchBuckets {
		depth := f.outbox.Depth(PipelineHistory)
		f.metrics.RecordQueueDrop(buckets, depth)
		f.logQueueDrop(entry, "invalid_bucket_count", nil)
		return false
	}

	f.stateMu.RLock()
	closed := f.closed
	f.stateMu.RUnlock()
	if closed {
		depth := f.outbox.Depth(PipelineHistory)
		f.metrics.RecordQueueDrop(buckets, depth)
		f.logQueueDrop(entry, "forwarder_stopped", nil)
		return false
	}

	depth, err := f.outbox.EnqueueHistory(f.server, entry, f.capacity)
	if err != nil {
		f.metrics.RecordQueueDrop(buckets, depth)
		reason := "outbox_write_failed"
		if errors.Is(err, ErrOutboxFull) {
			reason = "outbox_full"
		}
		f.logQueueDrop(entry, reason, err)
		return false
	}
	f.metrics.RecordEnqueued(buckets, depth)
	if depth >= f.batchSize || buckets >= f.maxBatchBuckets {
		f.signal()
	}
	return true
}

func (f *HistoryForwarder) Snapshot() HistoryForwarderSnapshot {
	depth := f.outbox.Depth(PipelineHistory)
	snapshot := f.metrics.Snapshot(depth, f.capacity)
	snapshot.Outbox = f.outbox.Snapshot(PipelineHistory)
	if snapshot.Outbox.DeadLetterBatches > 0 {
		snapshot.Status = "degraded"
	}
	return snapshot
}

func (f *HistoryForwarder) signal() {
	select {
	case f.wake <- struct{}{}:
	default:
	}
}

func (f *HistoryForwarder) logQueueDrop(entry HistoryIngest, reason string, err error) {
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
		observability.F("buckets", len(entry.History)),
		observability.F("outbox_depth", f.outbox.Depth(PipelineHistory)),
		observability.F("outbox_capacity", f.capacity),
	}
	if err != nil {
		fields = append(fields, observability.F("error", err))
	}
	f.logger.Event(observability.LevelDrop, "upstream.history_queue_drop", fields...)
}

func (f *HistoryForwarder) run(ctx context.Context) {
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

func (f *HistoryForwarder) processAvailable(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}
		batch, err := f.outbox.ClaimHistoryBatch(f.server, f.batchSize, f.maxBatchBuckets)
		if err != nil {
			f.logger.Event(observability.LevelError, "upstream.outbox_claim_failed", observability.F("pipeline", PipelineHistory), observability.F("error", err))
			return
		}
		if batch == nil {
			return
		}
		f.processBatch(ctx, batch)
	}
}

func (f *HistoryForwarder) processBatch(ctx context.Context, batch *ClaimedBatch) {
	payload := IngestHistoryRequest{
		RequestID: batch.RequestID,
		Server:    batch.Server,
		Entries:   batch.HistoryEntries,
	}
	entries := len(payload.Entries)
	buckets := historyBucketCount(payload.Entries)
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
		f.metrics.RecordSuccess(entries, buckets, duration, attemptsUsed, result.StatusCode)
		event := "upstream.history_batch_sent"
		if attemptsUsed > 1 || batch.Attempts > 0 || result.Response.Duplicate {
			event = "upstream.history_batch_recovered"
		}
		f.logger.Event(
			observability.LevelOK,
			event,
			observability.F("request_id", payload.RequestID),
			observability.F("server", payload.Server),
			observability.F("entries", entries),
			observability.F("buckets", buckets),
			observability.F("accepted_entries", result.Response.AcceptedEntries),
			observability.F("accepted_buckets", result.Response.AcceptedBuckets),
			observability.F("history_rows_touched", result.Response.HistoryRowsTouched),
			observability.F("duplicate", result.Response.Duplicate),
			observability.F("http_status", result.StatusCode),
			observability.F("attempts_this_cycle", attemptsUsed),
			observability.F("attempts_before_cycle", batch.Attempts),
			observability.F("duration_ms", milliseconds(duration)),
			observability.F("outbox_depth", f.outbox.Depth(PipelineHistory)),
		)
		return
	}

	statusCode, _ := SendErrorDetails(lastErr)
	totalAttempts := batch.Attempts + attemptsUsed
	if permanent || totalAttempts >= f.maxDeliveryAttempts {
		if err := f.outbox.DeadLetter(batch.RequestID, attemptsUsed, statusCode, lastErr.Error()); err != nil {
			f.logger.Event(observability.LevelError, "upstream.outbox_dead_letter_failed", observability.F("request_id", batch.RequestID), observability.F("error", err))
		}
		f.metrics.RecordDeadLetter(entries, buckets, duration, statusCode, lastErr)
		f.logger.Event(
			observability.LevelDrop,
			"upstream.history_batch_dead_lettered",
			observability.F("request_id", payload.RequestID),
			observability.F("entries", entries),
			observability.F("buckets", buckets),
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
		"upstream.history_batch_persisted_for_retry",
		observability.F("request_id", payload.RequestID),
		observability.F("entries", entries),
		observability.F("buckets", buckets),
		observability.F("attempts_total", totalAttempts),
		observability.F("retry_at", nextAttempt),
		observability.F("retry_in_ms", milliseconds(delay)),
		observability.F("http_status", statusCode),
		observability.F("error", lastErr),
	)
}

func (f *HistoryForwarder) sendCycle(ctx context.Context, payload IngestHistoryRequest) (HistorySendResult, int, error, bool) {
	var lastErr error
	var lastResult HistorySendResult
	for attempt := 1; attempt <= f.retryCount; attempt++ {
		if ctx.Err() != nil {
			return lastResult, attempt - 1, ctx.Err(), false
		}
		attemptStartedAt := time.Now()
		result, err := f.client.SendHistory(ctx, payload)
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
			"upstream.history_retry_scheduled",
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

func historyBucketCount(entries []HistoryIngest) int {
	total := 0
	for _, entry := range entries {
		total += len(entry.History)
	}
	return total
}

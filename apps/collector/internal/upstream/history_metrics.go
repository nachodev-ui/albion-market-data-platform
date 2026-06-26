package upstream

import (
	"sync"
	"time"
)

type HistoryTotalsSnapshot struct {
	EnqueuedEntries          uint64 `json:"enqueued_entries"`
	EnqueuedBuckets          uint64 `json:"enqueued_buckets"`
	QueueDroppedEntries      uint64 `json:"queue_dropped_entries"`
	QueueDroppedBuckets      uint64 `json:"queue_dropped_buckets"`
	BatchesStarted           uint64 `json:"batches_started"`
	BatchesSent              uint64 `json:"batches_sent"`
	EntriesSent              uint64 `json:"entries_sent"`
	BucketsSent              uint64 `json:"buckets_sent"`
	SendAttempts             uint64 `json:"send_attempts"`
	Retries                  uint64 `json:"retries"`
	RecoveredBatches         uint64 `json:"recovered_batches"`
	RescheduledBatches       uint64 `json:"rescheduled_batches"`
	FailedBatches            uint64 `json:"failed_batches"`
	DeadLetterBatches        uint64 `json:"dead_letter_batches"`
	EntriesDroppedAfterRetry uint64 `json:"entries_dropped_after_retries"`
	BucketsDroppedAfterRetry uint64 `json:"buckets_dropped_after_retries"`
}

type HistoryForwarderSnapshot struct {
	Enabled         bool                   `json:"enabled"`
	Running         bool                   `json:"running"`
	Status          string                 `json:"status"`
	InFlightBatches int                    `json:"in_flight_batches"`
	Queue           QueueSnapshot          `json:"queue"`
	Outbox          OutboxPipelineSnapshot `json:"outbox"`
	Totals          HistoryTotalsSnapshot  `json:"totals"`
	LatencyMS       LatencySnapshot        `json:"latency_ms"`
	LastStatusCode  int                    `json:"last_status_code,omitempty"`
	LastSuccessAt   *time.Time             `json:"last_success_at,omitempty"`
	LastErrorAt     *time.Time             `json:"last_error_at,omitempty"`
	LastError       string                 `json:"last_error,omitempty"`
}

type HistoryMetrics struct {
	mu sync.RWMutex

	running  bool
	inFlight int

	enqueuedEntries          uint64
	enqueuedBuckets          uint64
	queueDroppedEntries      uint64
	queueDroppedBuckets      uint64
	queueHighWatermark       int
	batchesStarted           uint64
	batchesSent              uint64
	entriesSent              uint64
	bucketsSent              uint64
	sendAttempts             uint64
	retries                  uint64
	recoveredBatches         uint64
	rescheduledBatches       uint64
	failedBatches            uint64
	deadLetterBatches        uint64
	entriesDroppedAfterRetry uint64
	bucketsDroppedAfterRetry uint64

	completedBatches   uint64
	totalBatchLatency  time.Duration
	lastBatchLatency   time.Duration
	maxBatchLatency    time.Duration
	lastAttemptLatency time.Duration

	lastStatusCode int
	lastSuccessAt  time.Time
	lastErrorAt    time.Time
	lastError      string
}

func NewHistoryMetrics() *HistoryMetrics {
	return &HistoryMetrics{}
}

func (m *HistoryMetrics) SetRunning(running bool) {
	m.mu.Lock()
	m.running = running
	m.mu.Unlock()
}

func (m *HistoryMetrics) RecordEnqueued(buckets, queueDepth int) {
	m.mu.Lock()
	m.enqueuedEntries++
	m.enqueuedBuckets += uint64(maxInt(buckets, 0))
	if queueDepth > m.queueHighWatermark {
		m.queueHighWatermark = queueDepth
	}
	m.mu.Unlock()
}

func (m *HistoryMetrics) RecordQueueDrop(buckets, queueDepth int) {
	m.mu.Lock()
	m.queueDroppedEntries++
	m.queueDroppedBuckets += uint64(maxInt(buckets, 0))
	if queueDepth > m.queueHighWatermark {
		m.queueHighWatermark = queueDepth
	}
	m.mu.Unlock()
}

func (m *HistoryMetrics) BeginBatch() {
	m.mu.Lock()
	m.batchesStarted++
	m.inFlight++
	m.mu.Unlock()
}

func (m *HistoryMetrics) RecordAttempt(duration time.Duration, statusCode int, err error) {
	now := time.Now().UTC()
	m.mu.Lock()
	m.sendAttempts++
	m.lastAttemptLatency = duration
	if statusCode != 0 {
		m.lastStatusCode = statusCode
	}
	if err != nil {
		m.lastErrorAt = now
		m.lastError = sanitizeStatusError(err.Error())
	}
	m.mu.Unlock()
}

func (m *HistoryMetrics) RecordRetry() {
	m.mu.Lock()
	m.retries++
	m.mu.Unlock()
}

func (m *HistoryMetrics) RecordSuccess(entries, buckets int, duration time.Duration, attempts, statusCode int) {
	now := time.Now().UTC()
	m.mu.Lock()
	if m.inFlight > 0 {
		m.inFlight--
	}
	m.batchesSent++
	m.entriesSent += uint64(maxInt(entries, 0))
	m.bucketsSent += uint64(maxInt(buckets, 0))
	if attempts > 1 {
		m.recoveredBatches++
	}
	m.recordBatchLatencyLocked(duration)
	if statusCode != 0 {
		m.lastStatusCode = statusCode
	}
	m.lastSuccessAt = now
	m.mu.Unlock()
}

func (m *HistoryMetrics) RecordDeferred(duration time.Duration, statusCode int, err error) {
	now := time.Now().UTC()
	m.mu.Lock()
	if m.inFlight > 0 {
		m.inFlight--
	}
	m.rescheduledBatches++
	m.recordBatchLatencyLocked(duration)
	if statusCode != 0 {
		m.lastStatusCode = statusCode
	}
	m.lastErrorAt = now
	if err != nil {
		m.lastError = sanitizeStatusError(err.Error())
	}
	m.mu.Unlock()
}

func (m *HistoryMetrics) RecordDeadLetter(entries, buckets int, duration time.Duration, statusCode int, err error) {
	now := time.Now().UTC()
	m.mu.Lock()
	if m.inFlight > 0 {
		m.inFlight--
	}
	m.failedBatches++
	m.deadLetterBatches++
	m.entriesDroppedAfterRetry += uint64(maxInt(entries, 0))
	m.bucketsDroppedAfterRetry += uint64(maxInt(buckets, 0))
	m.recordBatchLatencyLocked(duration)
	if statusCode != 0 {
		m.lastStatusCode = statusCode
	}
	m.lastErrorAt = now
	if err != nil {
		m.lastError = sanitizeStatusError(err.Error())
	}
	m.mu.Unlock()
}

func (m *HistoryMetrics) RecordFailure(entries, buckets int, duration time.Duration, statusCode int, err error) {
	now := time.Now().UTC()
	m.mu.Lock()
	if m.inFlight > 0 {
		m.inFlight--
	}
	m.failedBatches++
	m.entriesDroppedAfterRetry += uint64(maxInt(entries, 0))
	m.bucketsDroppedAfterRetry += uint64(maxInt(buckets, 0))
	m.recordBatchLatencyLocked(duration)
	if statusCode != 0 {
		m.lastStatusCode = statusCode
	}
	m.lastErrorAt = now
	if err != nil {
		m.lastError = sanitizeStatusError(err.Error())
	}
	m.mu.Unlock()
}

func (m *HistoryMetrics) recordBatchLatencyLocked(duration time.Duration) {
	m.completedBatches++
	m.totalBatchLatency += duration
	m.lastBatchLatency = duration
	if duration > m.maxBatchLatency {
		m.maxBatchLatency = duration
	}
}

func (m *HistoryMetrics) Snapshot(queueDepth, queueCapacity int) HistoryForwarderSnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()

	utilization := 0.0
	if queueCapacity > 0 {
		utilization = float64(queueDepth) * 100 / float64(queueCapacity)
	}
	average := time.Duration(0)
	if m.completedBatches > 0 {
		average = time.Duration(int64(m.totalBatchLatency) / int64(m.completedBatches))
	}

	snapshot := HistoryForwarderSnapshot{
		Enabled:         true,
		Running:         m.running,
		Status:          statusForSnapshot(m.running, utilization, m.lastSuccessAt, m.lastErrorAt),
		InFlightBatches: m.inFlight,
		Queue: QueueSnapshot{
			Depth:              queueDepth,
			Capacity:           queueCapacity,
			UtilizationPercent: roundMilliseconds(utilization),
			HighWatermark:      maxInt(m.queueHighWatermark, queueDepth),
		},
		Totals: HistoryTotalsSnapshot{
			EnqueuedEntries:          m.enqueuedEntries,
			EnqueuedBuckets:          m.enqueuedBuckets,
			QueueDroppedEntries:      m.queueDroppedEntries,
			QueueDroppedBuckets:      m.queueDroppedBuckets,
			BatchesStarted:           m.batchesStarted,
			BatchesSent:              m.batchesSent,
			EntriesSent:              m.entriesSent,
			BucketsSent:              m.bucketsSent,
			SendAttempts:             m.sendAttempts,
			Retries:                  m.retries,
			RecoveredBatches:         m.recoveredBatches,
			RescheduledBatches:       m.rescheduledBatches,
			FailedBatches:            m.failedBatches,
			DeadLetterBatches:        m.deadLetterBatches,
			EntriesDroppedAfterRetry: m.entriesDroppedAfterRetry,
			BucketsDroppedAfterRetry: m.bucketsDroppedAfterRetry,
		},
		LatencyMS: LatencySnapshot{
			LastBatchMS:    durationMilliseconds(m.lastBatchLatency),
			AverageBatchMS: durationMilliseconds(average),
			MaxBatchMS:     durationMilliseconds(m.maxBatchLatency),
			LastAttemptMS:  durationMilliseconds(m.lastAttemptLatency),
		},
		LastStatusCode: m.lastStatusCode,
		LastError:      m.lastError,
	}
	if !m.lastSuccessAt.IsZero() {
		value := m.lastSuccessAt
		snapshot.LastSuccessAt = &value
	}
	if !m.lastErrorAt.IsZero() {
		value := m.lastErrorAt
		snapshot.LastErrorAt = &value
	}
	return snapshot
}

func DisabledHistorySnapshot(queueCapacity int) HistoryForwarderSnapshot {
	return HistoryForwarderSnapshot{
		Enabled: false,
		Running: false,
		Status:  "disabled",
		Queue:   QueueSnapshot{Capacity: queueCapacity},
	}
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}

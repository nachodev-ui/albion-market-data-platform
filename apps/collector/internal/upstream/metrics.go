package upstream

import (
	"strings"
	"sync"
	"time"
)

const maxStatusErrorLength = 512

type QueueSnapshot struct {
	Depth              int     `json:"depth"`
	Capacity           int     `json:"capacity"`
	UtilizationPercent float64 `json:"utilization_percent"`
	HighWatermark      int     `json:"high_watermark"`
}

type TotalsSnapshot struct {
	EnqueuedEntries          uint64 `json:"enqueued_entries"`
	QueueDroppedEntries      uint64 `json:"queue_dropped_entries"`
	BatchesStarted           uint64 `json:"batches_started"`
	BatchesSent              uint64 `json:"batches_sent"`
	EntriesSent              uint64 `json:"entries_sent"`
	SendAttempts             uint64 `json:"send_attempts"`
	Retries                  uint64 `json:"retries"`
	RecoveredBatches         uint64 `json:"recovered_batches"`
	RescheduledBatches       uint64 `json:"rescheduled_batches"`
	FailedBatches            uint64 `json:"failed_batches"`
	DeadLetterBatches        uint64 `json:"dead_letter_batches"`
	EntriesDroppedAfterRetry uint64 `json:"entries_dropped_after_retries"`
}

type LatencySnapshot struct {
	LastBatchMS    float64 `json:"last_batch_ms"`
	AverageBatchMS float64 `json:"average_batch_ms"`
	MaxBatchMS     float64 `json:"max_batch_ms"`
	LastAttemptMS  float64 `json:"last_attempt_ms"`
}

type ForwarderSnapshot struct {
	Enabled         bool                   `json:"enabled"`
	Running         bool                   `json:"running"`
	Status          string                 `json:"status"`
	InFlightBatches int                    `json:"in_flight_batches"`
	Queue           QueueSnapshot          `json:"queue"`
	Outbox          OutboxPipelineSnapshot `json:"outbox"`
	Totals          TotalsSnapshot         `json:"totals"`
	LatencyMS       LatencySnapshot        `json:"latency_ms"`
	LastStatusCode  int                    `json:"last_status_code,omitempty"`
	LastSuccessAt   *time.Time             `json:"last_success_at,omitempty"`
	LastErrorAt     *time.Time             `json:"last_error_at,omitempty"`
	LastError       string                 `json:"last_error,omitempty"`
}

type Metrics struct {
	mu sync.RWMutex

	running  bool
	inFlight int

	enqueuedEntries          uint64
	queueDroppedEntries      uint64
	queueHighWatermark       int
	batchesStarted           uint64
	batchesSent              uint64
	entriesSent              uint64
	sendAttempts             uint64
	retries                  uint64
	recoveredBatches         uint64
	rescheduledBatches       uint64
	failedBatches            uint64
	deadLetterBatches        uint64
	entriesDroppedAfterRetry uint64

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

func NewMetrics() *Metrics {
	return &Metrics{}
}

func (m *Metrics) SetRunning(running bool) {
	m.mu.Lock()
	m.running = running
	m.mu.Unlock()
}

func (m *Metrics) RecordEnqueued(queueDepth int) {
	m.RecordEnqueuedMany(1, queueDepth)
}

func (m *Metrics) RecordEnqueuedMany(entries, queueDepth int) {
	m.mu.Lock()
	m.enqueuedEntries += uint64(maxInt(entries, 0))
	if queueDepth > m.queueHighWatermark {
		m.queueHighWatermark = queueDepth
	}
	m.mu.Unlock()
}

func (m *Metrics) RecordQueueDrop(queueDepth int) {
	m.RecordQueueDropMany(1, queueDepth)
}

func (m *Metrics) RecordQueueDropMany(entries, queueDepth int) {
	m.mu.Lock()
	m.queueDroppedEntries += uint64(maxInt(entries, 0))
	if queueDepth > m.queueHighWatermark {
		m.queueHighWatermark = queueDepth
	}
	m.mu.Unlock()
}

func (m *Metrics) BeginBatch() {
	m.mu.Lock()
	m.batchesStarted++
	m.inFlight++
	m.mu.Unlock()
}

func (m *Metrics) RecordAttempt(duration time.Duration, statusCode int, err error) {
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

func (m *Metrics) RecordRetry() {
	m.mu.Lock()
	m.retries++
	m.mu.Unlock()
}

func (m *Metrics) RecordSuccess(entries int, duration time.Duration, attempts int, statusCode int) {
	now := time.Now().UTC()
	m.mu.Lock()
	if m.inFlight > 0 {
		m.inFlight--
	}
	m.batchesSent++
	m.entriesSent += uint64(entries)
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

func (m *Metrics) RecordDeferred(duration time.Duration, statusCode int, err error) {
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

func (m *Metrics) RecordDeadLetter(entries int, duration time.Duration, statusCode int, err error) {
	now := time.Now().UTC()
	m.mu.Lock()
	if m.inFlight > 0 {
		m.inFlight--
	}
	m.failedBatches++
	m.deadLetterBatches++
	m.entriesDroppedAfterRetry += uint64(maxInt(entries, 0))
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

func (m *Metrics) RecordFailure(entries int, duration time.Duration, statusCode int, err error) {
	now := time.Now().UTC()
	m.mu.Lock()
	if m.inFlight > 0 {
		m.inFlight--
	}
	m.failedBatches++
	m.entriesDroppedAfterRetry += uint64(entries)
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

func (m *Metrics) recordBatchLatencyLocked(duration time.Duration) {
	m.completedBatches++
	m.totalBatchLatency += duration
	m.lastBatchLatency = duration
	if duration > m.maxBatchLatency {
		m.maxBatchLatency = duration
	}
}

func (m *Metrics) Snapshot(queueDepth, queueCapacity int) ForwarderSnapshot {
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

	snapshot := ForwarderSnapshot{
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
		Totals: TotalsSnapshot{
			EnqueuedEntries:          m.enqueuedEntries,
			QueueDroppedEntries:      m.queueDroppedEntries,
			BatchesStarted:           m.batchesStarted,
			BatchesSent:              m.batchesSent,
			EntriesSent:              m.entriesSent,
			SendAttempts:             m.sendAttempts,
			Retries:                  m.retries,
			RecoveredBatches:         m.recoveredBatches,
			RescheduledBatches:       m.rescheduledBatches,
			FailedBatches:            m.failedBatches,
			DeadLetterBatches:        m.deadLetterBatches,
			EntriesDroppedAfterRetry: m.entriesDroppedAfterRetry,
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

func DisabledSnapshot(queueCapacity int) ForwarderSnapshot {
	return ForwarderSnapshot{
		Enabled: false,
		Running: false,
		Status:  "disabled",
		Queue:   QueueSnapshot{Capacity: queueCapacity},
	}
}

func statusForSnapshot(running bool, utilization float64, lastSuccess, lastError time.Time) string {
	if !running {
		return "stopped"
	}
	if utilization >= 90 {
		return "degraded"
	}
	if !lastError.IsZero() && (lastSuccess.IsZero() || lastError.After(lastSuccess)) {
		return "degraded"
	}
	if lastSuccess.IsZero() {
		return "idle"
	}
	return "ok"
}

func sanitizeStatusError(value string) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	if len(value) > maxStatusErrorLength {
		value = value[:maxStatusErrorLength] + "..."
	}
	return value
}

func durationMilliseconds(value time.Duration) float64 {
	return roundMilliseconds(float64(value) / float64(time.Millisecond))
}

func roundMilliseconds(value float64) float64 {
	if value < 0 {
		return 0
	}
	return float64(int64(value*1000+0.5)) / 1000
}

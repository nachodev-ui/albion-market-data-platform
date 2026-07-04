package httpapi

import (
	"strconv"
	"strings"
	"time"

	"albion-market-data/collector/internal/upstream"
)

type forwarderMetricsView struct {
	enabled              bool
	running              bool
	status               string
	batchesSent          uint64
	recoveredBatches     uint64
	retries              uint64
	outboxRecovered      uint64
	depth                int
	capacity             int
	oldestPendingSeconds int64
	deadLetterCurrent    int
	deadLetterTotal      uint64
	lastBatchMS          float64
	averageBatchMS       float64
	maxBatchMS           float64
	lastAttemptMS        float64
	lastSuccessAt        *time.Time
	lastErrorAt          *time.Time
}

func forwarderViewFromPrice(snapshot upstream.ForwarderSnapshot) forwarderMetricsView {
	return forwarderMetricsView{
		enabled:              snapshot.Enabled,
		running:              snapshot.Running,
		status:               snapshot.Status,
		batchesSent:          snapshot.Totals.BatchesSent,
		recoveredBatches:     snapshot.Totals.RecoveredBatches,
		retries:              snapshot.Totals.Retries,
		outboxRecovered:      snapshot.Outbox.RecoveredBatchesTotal,
		depth:                snapshot.Queue.Depth,
		capacity:             snapshot.Queue.Capacity,
		oldestPendingSeconds: snapshot.Outbox.OldestPendingAgeSeconds,
		deadLetterCurrent:    snapshot.Outbox.DeadLetterBatches,
		deadLetterTotal:      snapshot.Outbox.DeadLetterBatchesTotal,
		lastBatchMS:          snapshot.LatencyMS.LastBatchMS,
		averageBatchMS:       snapshot.LatencyMS.AverageBatchMS,
		maxBatchMS:           snapshot.LatencyMS.MaxBatchMS,
		lastAttemptMS:        snapshot.LatencyMS.LastAttemptMS,
		lastSuccessAt:        snapshot.LastSuccessAt,
		lastErrorAt:          snapshot.LastErrorAt,
	}
}

func forwarderViewFromHistory(snapshot upstream.HistoryForwarderSnapshot) forwarderMetricsView {
	return forwarderMetricsView{
		enabled:              snapshot.Enabled,
		running:              snapshot.Running,
		status:               snapshot.Status,
		batchesSent:          snapshot.Totals.BatchesSent,
		recoveredBatches:     snapshot.Totals.RecoveredBatches,
		retries:              snapshot.Totals.Retries,
		outboxRecovered:      snapshot.Outbox.RecoveredBatchesTotal,
		depth:                snapshot.Queue.Depth,
		capacity:             snapshot.Queue.Capacity,
		oldestPendingSeconds: snapshot.Outbox.OldestPendingAgeSeconds,
		deadLetterCurrent:    snapshot.Outbox.DeadLetterBatches,
		deadLetterTotal:      snapshot.Outbox.DeadLetterBatchesTotal,
		lastBatchMS:          snapshot.LatencyMS.LastBatchMS,
		averageBatchMS:       snapshot.LatencyMS.AverageBatchMS,
		maxBatchMS:           snapshot.LatencyMS.MaxBatchMS,
		lastAttemptMS:        snapshot.LatencyMS.LastAttemptMS,
		lastSuccessAt:        snapshot.LastSuccessAt,
		lastErrorAt:          snapshot.LastErrorAt,
	}
}

func writeForwarderMetrics(output *strings.Builder, pipeline string, view forwarderMetricsView) {
	labels := map[string]string{"pipeline": pipeline}
	writeMetricHeaderOnce(output, "albion_receiver_forwarder_enabled", "Whether the upstream forwarder is enabled.", "gauge", pipeline)
	writeMetric(output, "albion_receiver_forwarder_enabled", labels, boolMetric(view.enabled))
	writeMetricHeaderOnce(output, "albion_receiver_forwarder_running", "Whether the upstream forwarder worker is running.", "gauge", pipeline)
	writeMetric(output, "albion_receiver_forwarder_running", labels, boolMetric(view.running))
	writeMetricHeaderOnce(output, "albion_receiver_forwarder_status", "Current upstream forwarder status.", "gauge", pipeline)
	writeMetric(output, "albion_receiver_forwarder_status", map[string]string{"pipeline": pipeline, "status": defaultMetricLabel(view.status, "unknown")}, "1")
	writeMetricHeaderOnce(output, "albion_receiver_forwarder_batches_sent_total", "Upstream batches sent successfully.", "counter", pipeline)
	writeMetric(output, "albion_receiver_forwarder_batches_sent_total", labels, strconv.FormatUint(view.batchesSent, 10))
	writeMetricHeaderOnce(output, "albion_receiver_forwarder_batches_recovered_total", "Upstream batches that succeeded after retry.", "counter", pipeline)
	writeMetric(output, "albion_receiver_forwarder_batches_recovered_total", labels, strconv.FormatUint(view.recoveredBatches, 10))
	writeMetricHeaderOnce(output, "albion_receiver_forwarder_retries_total", "Upstream retry attempts scheduled by the forwarder.", "counter", pipeline)
	writeMetric(output, "albion_receiver_forwarder_retries_total", labels, strconv.FormatUint(view.retries, 10))
	writeMetricHeaderOnce(output, "albion_receiver_outbox_recovered_batches_total", "Persistent outbox batches recovered after restart.", "counter", pipeline)
	writeMetric(output, "albion_receiver_outbox_recovered_batches_total", labels, strconv.FormatUint(view.outboxRecovered, 10))
	writeMetricHeaderOnce(output, "albion_receiver_outbox_depth", "Current persistent outbox entry depth.", "gauge", pipeline)
	writeMetric(output, "albion_receiver_outbox_depth", labels, strconv.Itoa(view.depth))
	writeMetricHeaderOnce(output, "albion_receiver_outbox_capacity", "Configured persistent outbox entry capacity.", "gauge", pipeline)
	writeMetric(output, "albion_receiver_outbox_capacity", labels, strconv.Itoa(view.capacity))
	writeMetricHeaderOnce(output, "albion_receiver_outbox_oldest_pending_age_seconds", "Age in seconds of the oldest pending outbox entry or batch.", "gauge", pipeline)
	writeMetric(output, "albion_receiver_outbox_oldest_pending_age_seconds", labels, strconv.FormatInt(view.oldestPendingSeconds, 10))
	writeMetricHeaderOnce(output, "albion_receiver_outbox_dead_letter_batches", "Current dead-letter batches retained in the outbox.", "gauge", pipeline)
	writeMetric(output, "albion_receiver_outbox_dead_letter_batches", labels, strconv.Itoa(view.deadLetterCurrent))
	writeMetricHeaderOnce(output, "albion_receiver_dead_letter_batches_total", "Persistent batches sent to dead-letter over the lifetime of the outbox.", "counter", pipeline)
	writeMetric(output, "albion_receiver_dead_letter_batches_total", labels, strconv.FormatUint(view.deadLetterTotal, 10))
	writeMetricHeaderOnce(output, "albion_receiver_upstream_latency_seconds", "Observed upstream batch and attempt latency in seconds.", "gauge", pipeline)
	writeMetric(output, "albion_receiver_upstream_latency_seconds", map[string]string{"pipeline": pipeline, "stat": "last_batch"}, millisecondsToSeconds(view.lastBatchMS))
	writeMetric(output, "albion_receiver_upstream_latency_seconds", map[string]string{"pipeline": pipeline, "stat": "average_batch"}, millisecondsToSeconds(view.averageBatchMS))
	writeMetric(output, "albion_receiver_upstream_latency_seconds", map[string]string{"pipeline": pipeline, "stat": "max_batch"}, millisecondsToSeconds(view.maxBatchMS))
	writeMetric(output, "albion_receiver_upstream_latency_seconds", map[string]string{"pipeline": pipeline, "stat": "last_attempt"}, millisecondsToSeconds(view.lastAttemptMS))
	writeMetricHeaderOnce(output, "albion_receiver_upstream_last_success_timestamp_seconds", "Unix timestamp of the last successful upstream batch.", "gauge", pipeline)
	writeTimestampMetric(output, "albion_receiver_upstream_last_success_timestamp_seconds", labels, view.lastSuccessAt)
	writeMetricHeaderOnce(output, "albion_receiver_upstream_last_error_timestamp_seconds", "Unix timestamp of the last upstream error.", "gauge", pipeline)
	writeTimestampMetric(output, "albion_receiver_upstream_last_error_timestamp_seconds", labels, view.lastErrorAt)
}

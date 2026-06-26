package upstream

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type Pipeline string

const (
	PipelinePrices  Pipeline = "prices"
	PipelineHistory Pipeline = "history"
)

const outboxStateVersion = 1

var ErrOutboxFull = errors.New("persistent outbox is full")

type outboxItem struct {
	ID        string         `json:"id"`
	Pipeline  Pipeline       `json:"pipeline"`
	Server    string         `json:"server"`
	CreatedAt time.Time      `json:"created_at"`
	Price     *PriceIngest   `json:"price,omitempty"`
	History   *HistoryIngest `json:"history,omitempty"`
}

type outboxBatch struct {
	RequestID      string          `json:"request_id"`
	Pipeline       Pipeline        `json:"pipeline"`
	Server         string          `json:"server"`
	Status         string          `json:"status"`
	PriceEntries   []PriceIngest   `json:"price_entries,omitempty"`
	HistoryEntries []HistoryIngest `json:"history_entries,omitempty"`
	Attempts       int             `json:"attempts"`
	NextAttemptAt  time.Time       `json:"next_attempt_at,omitempty"`
	LastAttemptAt  *time.Time      `json:"last_attempt_at,omitempty"`
	LastStatusCode int             `json:"last_status_code,omitempty"`
	LastError      string          `json:"last_error,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
	DeadLetteredAt *time.Time      `json:"dead_lettered_at,omitempty"`
}

type outboxTotals struct {
	PriceItemsEnqueued   uint64 `json:"price_items_enqueued"`
	HistoryItemsEnqueued uint64 `json:"history_items_enqueued"`
	BatchesCompleted     uint64 `json:"batches_completed"`
	BatchesRescheduled   uint64 `json:"batches_rescheduled"`
	BatchesDeadLettered  uint64 `json:"batches_dead_lettered"`
	RecoveredBatches     uint64 `json:"recovered_batches"`
}

type outboxState struct {
	Version int           `json:"version"`
	Items   []outboxItem  `json:"items"`
	Batches []outboxBatch `json:"batches"`
	Totals  outboxTotals  `json:"totals"`
}

type Outbox struct {
	path string
	now  func() time.Time
	mu   sync.Mutex
	data outboxState
}

type ClaimedBatch struct {
	RequestID      string
	Pipeline       Pipeline
	Server         string
	PriceEntries   []PriceIngest
	HistoryEntries []HistoryIngest
	Attempts       int
	CreatedAt      time.Time
}

type OutboxPipelineSnapshot struct {
	Path                    string     `json:"path"`
	PendingEntries          int        `json:"pending_entries"`
	PendingBatches          int        `json:"pending_batches"`
	RetryingBatches         int        `json:"retrying_batches"`
	ProcessingBatches       int        `json:"processing_batches"`
	DeadLetterBatches       int        `json:"dead_letter_batches"`
	OldestPendingAt         *time.Time `json:"oldest_pending_at,omitempty"`
	OldestPendingAgeSeconds int64      `json:"oldest_pending_age_seconds"`
	RecoveredBatchesTotal   uint64     `json:"recovered_batches_total"`
	CompletedBatchesTotal   uint64     `json:"completed_batches_total"`
	RescheduledBatchesTotal uint64     `json:"rescheduled_batches_total"`
	DeadLetterBatchesTotal  uint64     `json:"dead_letter_batches_total"`
}

type DeadLetterRecord struct {
	RequestID      string    `json:"request_id"`
	Pipeline       Pipeline  `json:"pipeline"`
	Server         string    `json:"server"`
	Entries        int       `json:"entries"`
	Buckets        int       `json:"buckets,omitempty"`
	Attempts       int       `json:"attempts"`
	LastStatusCode int       `json:"last_status_code,omitempty"`
	LastError      string    `json:"last_error,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	DeadLetteredAt time.Time `json:"dead_lettered_at"`
}

func NewOutbox(path string) (*Outbox, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("outbox path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create outbox directory: %w", err)
	}
	outbox := &Outbox{
		path: path,
		now:  time.Now,
		data: outboxState{Version: outboxStateVersion},
	}
	if err := outbox.load(); err != nil {
		return nil, err
	}
	if err := outbox.recoverProcessing(); err != nil {
		return nil, err
	}
	return outbox, nil
}

func (o *Outbox) Path() string { return o.path }

func (o *Outbox) EnqueuePrice(server string, entry PriceIngest, capacity int) (int, error) {
	accepted, depth, err := o.EnqueuePrices(server, []PriceIngest{entry}, capacity)
	if err != nil {
		return depth, err
	}
	if accepted != 1 {
		return depth, ErrOutboxFull
	}
	return depth, nil
}

func (o *Outbox) EnqueuePrices(server string, entries []PriceIngest, capacity int) (accepted int, depth int, err error) {
	if len(entries) == 0 {
		return 0, o.Depth(PipelinePrices), nil
	}
	o.mu.Lock()
	defer o.mu.Unlock()

	currentDepth := o.depthLocked(PipelinePrices)
	available := len(entries)
	if capacity > 0 {
		available = capacity - currentDepth
		if available < 0 {
			available = 0
		}
		if available > len(entries) {
			available = len(entries)
		}
	}
	if available == 0 {
		return 0, currentDepth, ErrOutboxFull
	}

	beforeLength := len(o.data.Items)
	beforeTotal := o.data.Totals.PriceItemsEnqueued
	now := o.now().UTC()
	for index := 0; index < available; index++ {
		entry := entries[index]
		o.data.Items = append(o.data.Items, outboxItem{
			ID:        newRequestID(),
			Pipeline:  PipelinePrices,
			Server:    server,
			CreatedAt: now,
			Price:     &entry,
		})
	}
	o.data.Totals.PriceItemsEnqueued += uint64(available)
	if err := o.persistLocked(); err != nil {
		o.data.Items = o.data.Items[:beforeLength]
		o.data.Totals.PriceItemsEnqueued = beforeTotal
		return 0, currentDepth, err
	}
	return available, currentDepth + available, nil
}

func (o *Outbox) EnqueueHistory(server string, entry HistoryIngest, capacity int) (int, error) {
	return o.enqueue(outboxItem{
		ID:        newRequestID(),
		Pipeline:  PipelineHistory,
		Server:    server,
		CreatedAt: o.now().UTC(),
		History:   &entry,
	}, capacity)
}

func (o *Outbox) enqueue(item outboxItem, capacity int) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	depth := o.depthLocked(item.Pipeline)
	if capacity > 0 && depth >= capacity {
		return depth, ErrOutboxFull
	}
	o.data.Items = append(o.data.Items, item)
	previousTotals := o.data.Totals
	switch item.Pipeline {
	case PipelinePrices:
		o.data.Totals.PriceItemsEnqueued++
	case PipelineHistory:
		o.data.Totals.HistoryItemsEnqueued++
	}
	if err := o.persistLocked(); err != nil {
		o.data.Items = o.data.Items[:len(o.data.Items)-1]
		o.data.Totals = previousTotals
		return depth, err
	}
	return depth + 1, nil
}

func (o *Outbox) ClaimPriceBatch(server string, maxEntries int) (*ClaimedBatch, error) {
	return o.claimBatch(PipelinePrices, server, maxEntries, 0)
}

func (o *Outbox) ClaimHistoryBatch(server string, maxEntries, maxBuckets int) (*ClaimedBatch, error) {
	return o.claimBatch(PipelineHistory, server, maxEntries, maxBuckets)
}

func (o *Outbox) claimBatch(pipeline Pipeline, server string, maxEntries, maxBuckets int) (*ClaimedBatch, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	now := o.now().UTC()
	for index := range o.data.Batches {
		batch := &o.data.Batches[index]
		if batch.Pipeline != pipeline || batch.Server != server || batch.Status == "dead_letter" {
			continue
		}
		if batch.Status == "processing" {
			continue
		}
		if !batch.NextAttemptAt.IsZero() && batch.NextAttemptAt.After(now) {
			continue
		}
		previous := *batch
		batch.Status = "processing"
		batch.UpdatedAt = now
		if err := o.persistLocked(); err != nil {
			*batch = previous
			return nil, err
		}
		return claimedFromBatch(*batch), nil
	}

	if maxEntries <= 0 {
		maxEntries = 1
	}
	selected := make([]int, 0, maxEntries)
	bucketCount := 0
	for index := range o.data.Items {
		item := o.data.Items[index]
		if item.Pipeline != pipeline || item.Server != server {
			continue
		}
		if len(selected) >= maxEntries {
			break
		}
		if pipeline == PipelineHistory {
			if item.History == nil {
				continue
			}
			itemBuckets := len(item.History.History)
			if maxBuckets > 0 && len(selected) > 0 && bucketCount+itemBuckets > maxBuckets {
				break
			}
			if maxBuckets > 0 && itemBuckets > maxBuckets {
				continue
			}
			bucketCount += itemBuckets
		}
		selected = append(selected, index)
	}
	if len(selected) == 0 {
		return nil, nil
	}

	batch := outboxBatch{
		RequestID: newRequestID(),
		Pipeline:  pipeline,
		Server:    server,
		Status:    "processing",
		CreatedAt: now,
		UpdatedAt: now,
	}
	selectedSet := make(map[int]struct{}, len(selected))
	for _, index := range selected {
		selectedSet[index] = struct{}{}
		item := o.data.Items[index]
		if item.Price != nil {
			batch.PriceEntries = append(batch.PriceEntries, *item.Price)
		}
		if item.History != nil {
			batch.HistoryEntries = append(batch.HistoryEntries, *item.History)
		}
	}
	previousItems := append([]outboxItem(nil), o.data.Items...)
	previousBatches := append([]outboxBatch(nil), o.data.Batches...)
	remaining := make([]outboxItem, 0, len(o.data.Items)-len(selected))
	for index, item := range o.data.Items {
		if _, exists := selectedSet[index]; !exists {
			remaining = append(remaining, item)
		}
	}
	o.data.Items = remaining
	o.data.Batches = append(o.data.Batches, batch)
	if err := o.persistLocked(); err != nil {
		o.data.Items = previousItems
		o.data.Batches = previousBatches
		return nil, err
	}
	return claimedFromBatch(batch), nil
}

func (o *Outbox) Complete(requestID string) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	for index := range o.data.Batches {
		if o.data.Batches[index].RequestID != requestID {
			continue
		}
		previousBatches := append([]outboxBatch(nil), o.data.Batches...)
		previousTotals := o.data.Totals
		o.data.Batches = append(o.data.Batches[:index], o.data.Batches[index+1:]...)
		o.data.Totals.BatchesCompleted++
		if err := o.persistLocked(); err != nil {
			o.data.Batches = previousBatches
			o.data.Totals = previousTotals
			return err
		}
		return nil
	}
	return fmt.Errorf("outbox batch %s not found", requestID)
}

func (o *Outbox) Reschedule(requestID string, attemptsUsed, statusCode int, lastError string, nextAttemptAt time.Time) error {
	return o.finishFailedAttempt(requestID, attemptsUsed, statusCode, lastError, nextAttemptAt, false)
}

func (o *Outbox) DeadLetter(requestID string, attemptsUsed, statusCode int, lastError string) error {
	return o.finishFailedAttempt(requestID, attemptsUsed, statusCode, lastError, time.Time{}, true)
}

func (o *Outbox) finishFailedAttempt(requestID string, attemptsUsed, statusCode int, lastError string, nextAttemptAt time.Time, dead bool) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	now := o.now().UTC()
	for index := range o.data.Batches {
		batch := &o.data.Batches[index]
		if batch.RequestID != requestID {
			continue
		}
		previous := *batch
		previousTotals := o.data.Totals
		batch.Attempts += maxInt(attemptsUsed, 0)
		batch.LastStatusCode = statusCode
		batch.LastError = sanitizeStatusError(lastError)
		batch.UpdatedAt = now
		batch.LastAttemptAt = &now
		if dead {
			batch.Status = "dead_letter"
			batch.NextAttemptAt = time.Time{}
			batch.DeadLetteredAt = &now
			o.data.Totals.BatchesDeadLettered++
		} else {
			batch.Status = "retrying"
			batch.NextAttemptAt = nextAttemptAt.UTC()
			o.data.Totals.BatchesRescheduled++
		}
		if err := o.persistLocked(); err != nil {
			*batch = previous
			o.data.Totals = previousTotals
			return err
		}
		return nil
	}
	return fmt.Errorf("outbox batch %s not found", requestID)
}

func (o *Outbox) Snapshot(pipeline Pipeline) OutboxPipelineSnapshot {
	o.mu.Lock()
	defer o.mu.Unlock()

	now := o.now().UTC()
	snapshot := OutboxPipelineSnapshot{
		Path:                    o.path,
		RecoveredBatchesTotal:   o.data.Totals.RecoveredBatches,
		CompletedBatchesTotal:   o.data.Totals.BatchesCompleted,
		RescheduledBatchesTotal: o.data.Totals.BatchesRescheduled,
		DeadLetterBatchesTotal:  o.data.Totals.BatchesDeadLettered,
	}
	var oldest time.Time
	for _, item := range o.data.Items {
		if item.Pipeline != pipeline {
			continue
		}
		snapshot.PendingEntries++
		if oldest.IsZero() || item.CreatedAt.Before(oldest) {
			oldest = item.CreatedAt
		}
	}
	for _, batch := range o.data.Batches {
		if batch.Pipeline != pipeline {
			continue
		}
		switch batch.Status {
		case "processing":
			snapshot.ProcessingBatches++
		case "retrying":
			snapshot.RetryingBatches++
		case "dead_letter":
			snapshot.DeadLetterBatches++
		default:
			snapshot.PendingBatches++
		}
		if batch.Status != "dead_letter" && (oldest.IsZero() || batch.CreatedAt.Before(oldest)) {
			oldest = batch.CreatedAt
		}
	}
	if !oldest.IsZero() {
		value := oldest
		snapshot.OldestPendingAt = &value
		age := now.Sub(oldest)
		if age > 0 {
			snapshot.OldestPendingAgeSeconds = int64(age / time.Second)
		}
	}
	return snapshot
}

func (o *Outbox) Depth(pipeline Pipeline) int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.depthLocked(pipeline)
}

func (o *Outbox) depthLocked(pipeline Pipeline) int {
	depth := 0
	for _, item := range o.data.Items {
		if item.Pipeline == pipeline {
			depth++
		}
	}
	for _, batch := range o.data.Batches {
		if batch.Pipeline != pipeline || batch.Status == "dead_letter" {
			continue
		}
		if pipeline == PipelinePrices {
			depth += len(batch.PriceEntries)
		} else {
			depth += len(batch.HistoryEntries)
		}
	}
	return depth
}

func (o *Outbox) ListDeadLetters(pipeline Pipeline) []DeadLetterRecord {
	o.mu.Lock()
	defer o.mu.Unlock()

	records := make([]DeadLetterRecord, 0)
	for _, batch := range o.data.Batches {
		if batch.Status != "dead_letter" || (pipeline != "" && batch.Pipeline != pipeline) || batch.DeadLetteredAt == nil {
			continue
		}
		records = append(records, DeadLetterRecord{
			RequestID:      batch.RequestID,
			Pipeline:       batch.Pipeline,
			Server:         batch.Server,
			Entries:        batchEntryCount(batch),
			Buckets:        historyBucketCount(batch.HistoryEntries),
			Attempts:       batch.Attempts,
			LastStatusCode: batch.LastStatusCode,
			LastError:      batch.LastError,
			CreatedAt:      batch.CreatedAt,
			DeadLetteredAt: *batch.DeadLetteredAt,
		})
	}
	sort.Slice(records, func(i, j int) bool { return records[i].DeadLetteredAt.Before(records[j].DeadLetteredAt) })
	return records
}

func (o *Outbox) RequeueDeadLetter(requestID string) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	now := o.now().UTC()
	for index := range o.data.Batches {
		batch := &o.data.Batches[index]
		if batch.RequestID != requestID || batch.Status != "dead_letter" {
			continue
		}
		previous := *batch
		batch.Status = "retrying"
		batch.NextAttemptAt = now
		batch.DeadLetteredAt = nil
		batch.Attempts = 0
		batch.LastAttemptAt = nil
		batch.LastError = ""
		batch.LastStatusCode = 0
		batch.UpdatedAt = now
		if err := o.persistLocked(); err != nil {
			*batch = previous
			return err
		}
		return nil
	}
	return fmt.Errorf("dead-letter batch %s not found", requestID)
}

func (o *Outbox) PurgeDeadLetter(requestID string) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	for index := range o.data.Batches {
		batch := o.data.Batches[index]
		if batch.RequestID == requestID && batch.Status == "dead_letter" {
			previousBatches := append([]outboxBatch(nil), o.data.Batches...)
			o.data.Batches = append(o.data.Batches[:index], o.data.Batches[index+1:]...)
			if err := o.persistLocked(); err != nil {
				o.data.Batches = previousBatches
				return err
			}
			return nil
		}
	}
	return fmt.Errorf("dead-letter batch %s not found", requestID)
}

func (o *Outbox) recoverProcessing() error {
	o.mu.Lock()
	defer o.mu.Unlock()

	now := o.now().UTC()
	previousBatches := append([]outboxBatch(nil), o.data.Batches...)
	previousTotals := o.data.Totals
	recovered := 0
	for index := range o.data.Batches {
		batch := &o.data.Batches[index]
		if batch.Status != "processing" {
			continue
		}
		batch.Status = "retrying"
		batch.NextAttemptAt = now
		batch.UpdatedAt = now
		recovered++
	}
	if recovered == 0 {
		return nil
	}
	o.data.Totals.RecoveredBatches += uint64(recovered)
	if err := o.persistLocked(); err != nil {
		o.data.Batches = previousBatches
		o.data.Totals = previousTotals
		return err
	}
	return nil
}

func (o *Outbox) load() error {
	o.mu.Lock()
	defer o.mu.Unlock()

	loaded, primaryErr := readOutboxState(o.path)
	if primaryErr != nil {
		backup := o.path + ".bak"
		loadedBackup, backupErr := readOutboxState(backup)
		if backupErr != nil {
			if errors.Is(primaryErr, os.ErrNotExist) && errors.Is(backupErr, os.ErrNotExist) {
				return nil
			}
			return fmt.Errorf("load outbox state (primary: %v, backup: %v)", primaryErr, backupErr)
		}
		loaded = loadedBackup
	}
	if loaded.Version != outboxStateVersion {
		return fmt.Errorf("unsupported outbox state version %d", loaded.Version)
	}
	o.data = loaded
	return nil
}

func readOutboxState(path string) (outboxState, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return outboxState{}, err
	}
	var state outboxState
	if err := json.Unmarshal(content, &state); err != nil {
		return outboxState{}, fmt.Errorf("decode %s: %w", path, err)
	}
	return state, nil
}

func (o *Outbox) persistLocked() error {
	o.data.Version = outboxStateVersion
	content, err := json.MarshalIndent(o.data, "", "  ")
	if err != nil {
		return fmt.Errorf("encode outbox state: %w", err)
	}

	temporary := o.path + ".tmp"
	backup := o.path + ".bak"
	file, err := os.OpenFile(temporary, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open temporary outbox state: %w", err)
	}
	if _, err := file.Write(content); err != nil {
		file.Close()
		return fmt.Errorf("write temporary outbox state: %w", err)
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return fmt.Errorf("sync temporary outbox state: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close temporary outbox state: %w", err)
	}

	_ = os.Remove(backup)
	if _, err := os.Stat(o.path); err == nil {
		if err := os.Rename(o.path, backup); err != nil {
			return fmt.Errorf("backup outbox state: %w", err)
		}
	}
	if err := os.Rename(temporary, o.path); err != nil {
		_ = os.Rename(backup, o.path)
		return fmt.Errorf("install outbox state: %w", err)
	}
	_ = os.Remove(backup)
	return nil
}

func claimedFromBatch(batch outboxBatch) *ClaimedBatch {
	return &ClaimedBatch{
		RequestID:      batch.RequestID,
		Pipeline:       batch.Pipeline,
		Server:         batch.Server,
		PriceEntries:   append([]PriceIngest(nil), batch.PriceEntries...),
		HistoryEntries: append([]HistoryIngest(nil), batch.HistoryEntries...),
		Attempts:       batch.Attempts,
		CreatedAt:      batch.CreatedAt,
	}
}

func batchEntryCount(batch outboxBatch) int {
	if batch.Pipeline == PipelinePrices {
		return len(batch.PriceEntries)
	}
	return len(batch.HistoryEntries)
}

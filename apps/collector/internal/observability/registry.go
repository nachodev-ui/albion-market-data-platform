package observability

import (
	"strings"
	"sync"
	"time"
)

const maxRegistryErrorLength = 512

type CounterSnapshot struct {
	Total  uint64
	LastAt *time.Time
}

type StorageSnapshot struct {
	WritesTotal uint64
	ErrorsTotal uint64
	LastWriteAt *time.Time
	LastErrorAt *time.Time
	LastError   string
}

type RegistrySnapshot struct {
	StartedAt           time.Time
	UptimeSeconds       int64
	CapturesReceived    map[string]CounterSnapshot
	CaptureBytes        map[string]uint64
	EntriesReceived     map[string]CounterSnapshot
	EntriesStored       map[string]CounterSnapshot
	Duplicates          map[string]CounterSnapshot
	NormalizationErrors map[string]CounterSnapshot
	Storage             map[string]StorageSnapshot
}

type counterState struct {
	total  uint64
	lastAt time.Time
}

type storageState struct {
	writesTotal uint64
	errorsTotal uint64
	lastWriteAt time.Time
	lastErrorAt time.Time
	lastError   string
}

// Registry stores process-local receiver metrics. It is intentionally independent
// from the upstream package so the same data can be consumed by status and
// Prometheus exporters without creating dependency cycles.
type Registry struct {
	mu sync.RWMutex

	startedAt time.Time
	now       func() time.Time

	capturesReceived    map[string]counterState
	captureBytes        map[string]uint64
	entriesReceived     map[string]counterState
	entriesStored       map[string]counterState
	duplicates          map[string]counterState
	normalizationErrors map[string]counterState
	storage             map[string]storageState
}

func NewRegistry(startedAt time.Time) *Registry {
	if startedAt.IsZero() {
		startedAt = time.Now().UTC()
	}
	return &Registry{
		startedAt:           startedAt.UTC(),
		now:                 time.Now,
		capturesReceived:    make(map[string]counterState),
		captureBytes:        make(map[string]uint64),
		entriesReceived:     make(map[string]counterState),
		entriesStored:       make(map[string]counterState),
		duplicates:          make(map[string]counterState),
		normalizationErrors: make(map[string]counterState),
		storage:             make(map[string]storageState),
	}
}

func (r *Registry) RecordCapture(topic string, bytes int) {
	if r == nil {
		return
	}
	topic = normalizeMetricDimension(topic, "unknown")
	now := r.now().UTC()
	r.mu.Lock()
	state := r.capturesReceived[topic]
	state.total++
	state.lastAt = now
	r.capturesReceived[topic] = state
	if bytes > 0 {
		r.captureBytes[topic] += uint64(bytes)
	}
	r.mu.Unlock()
}

func (r *Registry) RecordEntriesReceived(pipeline string, entries int) {
	if r == nil {
		return
	}
	r.recordCounter(r.entriesReceived, pipeline, entries)
}

func (r *Registry) RecordPersistence(pipeline string, stored, duplicates int) {
	if r == nil {
		return
	}
	r.recordCounter(r.entriesStored, pipeline, stored)
	r.recordCounter(r.duplicates, pipeline, duplicates)
}

func (r *Registry) RecordNormalizationError(pipeline string) {
	if r == nil {
		return
	}
	r.recordCounter(r.normalizationErrors, pipeline, 1)
}

func (r *Registry) RecordStorageSuccess(area string) {
	if r == nil {
		return
	}
	area = normalizeMetricDimension(area, "unknown")
	now := r.now().UTC()
	r.mu.Lock()
	state := r.storage[area]
	state.writesTotal++
	state.lastWriteAt = now
	r.storage[area] = state
	r.mu.Unlock()
}

func (r *Registry) RecordStorageError(area string, err error) {
	if r == nil {
		return
	}
	area = normalizeMetricDimension(area, "unknown")
	now := r.now().UTC()
	r.mu.Lock()
	state := r.storage[area]
	state.errorsTotal++
	state.lastErrorAt = now
	if err != nil {
		state.lastError = sanitizeRegistryError(err.Error())
	}
	r.storage[area] = state
	r.mu.Unlock()
}

func (r *Registry) Snapshot() RegistrySnapshot {
	if r == nil {
		return RegistrySnapshot{}
	}
	now := r.now().UTC()
	r.mu.RLock()
	defer r.mu.RUnlock()

	uptime := now.Sub(r.startedAt)
	if uptime < 0 {
		uptime = 0
	}
	return RegistrySnapshot{
		StartedAt:           r.startedAt,
		UptimeSeconds:       int64(uptime / time.Second),
		CapturesReceived:    copyCounterMap(r.capturesReceived),
		CaptureBytes:        copyUint64Map(r.captureBytes),
		EntriesReceived:     copyCounterMap(r.entriesReceived),
		EntriesStored:       copyCounterMap(r.entriesStored),
		Duplicates:          copyCounterMap(r.duplicates),
		NormalizationErrors: copyCounterMap(r.normalizationErrors),
		Storage:             copyStorageMap(r.storage),
	}
}

func (r *Registry) recordCounter(target map[string]counterState, dimension string, delta int) {
	if r == nil || delta <= 0 {
		return
	}
	dimension = normalizeMetricDimension(dimension, "unknown")
	now := r.now().UTC()
	r.mu.Lock()
	state := target[dimension]
	state.total += uint64(delta)
	state.lastAt = now
	target[dimension] = state
	r.mu.Unlock()
}

func copyCounterMap(source map[string]counterState) map[string]CounterSnapshot {
	result := make(map[string]CounterSnapshot, len(source))
	for key, state := range source {
		snapshot := CounterSnapshot{Total: state.total}
		if !state.lastAt.IsZero() {
			value := state.lastAt
			snapshot.LastAt = &value
		}
		result[key] = snapshot
	}
	return result
}

func copyUint64Map(source map[string]uint64) map[string]uint64 {
	result := make(map[string]uint64, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

func copyStorageMap(source map[string]storageState) map[string]StorageSnapshot {
	result := make(map[string]StorageSnapshot, len(source))
	for key, state := range source {
		snapshot := StorageSnapshot{
			WritesTotal: state.writesTotal,
			ErrorsTotal: state.errorsTotal,
			LastError:   state.lastError,
		}
		if !state.lastWriteAt.IsZero() {
			value := state.lastWriteAt
			snapshot.LastWriteAt = &value
		}
		if !state.lastErrorAt.IsZero() {
			value := state.lastErrorAt
			snapshot.LastErrorAt = &value
		}
		result[key] = snapshot
	}
	return result
}

func normalizeMetricDimension(value, fallback string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return fallback
	}
	return value
}

func sanitizeRegistryError(value string) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	if len(value) > maxRegistryErrorLength {
		value = value[:maxRegistryErrorLength] + "..."
	}
	return value
}

package observability

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type StoragePaths struct {
	RawDirectory        string
	NormalizedDirectory string
	DatabasePath        string
	OutboxPath          string
	MaxBytes            int64
}

type StorageUsageSnapshot struct {
	RawBytes        int64
	NormalizedBytes int64
	DatabaseBytes   int64
	OutboxBytes     int64
	TotalBytes      int64
	MaxBytes        int64
	MeasuredAt      time.Time
	Errors          map[string]string
}

type StorageUsage struct {
	paths StoragePaths
	ttl   time.Duration
	now   func() time.Time

	mu       sync.Mutex
	cachedAt time.Time
	cached   StorageUsageSnapshot
}

func NewStorageUsage(paths StoragePaths, ttl time.Duration) *StorageUsage {
	if ttl <= 0 {
		ttl = 5 * time.Second
	}
	return &StorageUsage{paths: paths, ttl: ttl, now: time.Now}
}

func (s *StorageUsage) Snapshot() StorageUsageSnapshot {
	if s == nil {
		return StorageUsageSnapshot{}
	}
	now := s.now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.cachedAt.IsZero() && now.Sub(s.cachedAt) >= 0 && now.Sub(s.cachedAt) < s.ttl {
		return copyStorageUsageSnapshot(s.cached)
	}

	result := StorageUsageSnapshot{
		MaxBytes:   s.paths.MaxBytes,
		MeasuredAt: now,
		Errors:     make(map[string]string),
	}
	measureDirectory := func(area, path string) int64 {
		value, err := directorySize(path)
		if err != nil {
			result.Errors[area] = sanitizeRegistryError(err.Error())
		}
		return value
	}
	measureFile := func(area, path string) int64 {
		value, err := fileSize(path)
		if err != nil {
			result.Errors[area] = sanitizeRegistryError(err.Error())
		}
		return value
	}
	result.RawBytes = measureDirectory("raw", s.paths.RawDirectory)
	result.NormalizedBytes = measureDirectory("normalized", s.paths.NormalizedDirectory)
	result.DatabaseBytes = measureFile("database", s.paths.DatabasePath)
	result.OutboxBytes = measureFile("outbox", s.paths.OutboxPath)
	result.TotalBytes = result.RawBytes + result.NormalizedBytes + result.DatabaseBytes + result.OutboxBytes

	s.cachedAt = now
	s.cached = copyStorageUsageSnapshot(result)
	return result
}

func directorySize(root string) (int64, error) {
	if root == "" {
		return 0, nil
	}
	var total int64
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		total += info.Size()
		return nil
	})
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	return total, err
}

func fileSize(path string) (int64, error) {
	if path == "" {
		return 0, nil
	}
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if !info.Mode().IsRegular() {
		return 0, nil
	}
	return info.Size(), nil
}

func copyStorageUsageSnapshot(source StorageUsageSnapshot) StorageUsageSnapshot {
	result := source
	result.Errors = make(map[string]string, len(source.Errors))
	for key, value := range source.Errors {
		result.Errors[key] = value
	}
	return result
}

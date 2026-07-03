package durable

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type RetentionPolicy struct {
	RawDays        int
	NormalizedDays int
	BackupDays     int
	MinimumBackups int
	MaxBytes       int64
}

type RetentionReport struct {
	DeletedFiles int
	DeletedBytes int64
	CurrentBytes int64
}

func EnforceRetention(dataRoot, backupRoot string, now time.Time, policy RetentionPolicy) (RetentionReport, error) {
	report := RetentionReport{}
	groups := []struct {
		directory, pattern string
		days               int
	}{
		{filepath.Join(dataRoot, "raw"), "raw-ingest-*.jsonl", policy.RawDays},
		{filepath.Join(dataRoot, "normalized"), "market-history-*.jsonl", policy.NormalizedDays},
		{filepath.Join(dataRoot, "normalized"), "market-orders-*.jsonl", policy.NormalizedDays},
	}
	for _, group := range groups {
		if group.days <= 0 {
			continue
		}
		paths, err := filepath.Glob(filepath.Join(group.directory, group.pattern))
		if err != nil {
			return report, err
		}
		cutoff := now.UTC().AddDate(0, 0, -group.days)
		for _, path := range paths {
			date, ok := dateFromFilename(filepath.Base(path))
			if !ok || !date.Before(cutoff) {
				continue
			}
			info, err := os.Stat(path)
			if err != nil {
				return report, err
			}
			if err := os.Remove(path); err != nil {
				return report, fmt.Errorf("remove retained file %s: %w", path, err)
			}
			report.DeletedFiles++
			report.DeletedBytes += info.Size()
		}
	}
	if policy.BackupDays > 0 && backupRoot != "" {
		entries, err := os.ReadDir(backupRoot)
		if err != nil && !os.IsNotExist(err) {
			return report, err
		}
		type backupFile struct {
			path string
			info os.FileInfo
		}
		backups := make([]backupFile, 0)
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".zip") {
				continue
			}
			info, err := entry.Info()
			if err != nil {
				return report, err
			}
			backups = append(backups, backupFile{filepath.Join(backupRoot, entry.Name()), info})
		}
		sort.Slice(backups, func(i, j int) bool { return backups[i].info.ModTime().After(backups[j].info.ModTime()) })
		cutoff := now.UTC().AddDate(0, 0, -policy.BackupDays)
		for index, backup := range backups {
			if index < policy.MinimumBackups || !backup.info.ModTime().Before(cutoff) {
				continue
			}
			if err := os.Remove(backup.path); err != nil {
				return report, err
			}
			_ = os.Remove(backup.path + ".sha256")
			report.DeletedFiles++
			report.DeletedBytes += backup.info.Size()
		}
	}
	if err := ConfigureBudget(dataRoot, policy.MaxBytes); err != nil {
		return report, err
	}
	size, err := DirectorySize(dataRoot)
	if err != nil {
		return report, err
	}
	report.CurrentBytes = size
	if policy.MaxBytes > 0 && size > policy.MaxBytes {
		return report, fmt.Errorf("%w: current=%d maximum=%d", ErrStorageLimit, size, policy.MaxBytes)
	}
	return report, nil
}

func dateFromFilename(name string) (time.Time, bool) {
	if len(name) < len("2006-01-02.jsonl") {
		return time.Time{}, false
	}
	stem := strings.TrimSuffix(name, filepath.Ext(name))
	if len(stem) < 10 {
		return time.Time{}, false
	}
	value := stem[len(stem)-10:]
	parsed, err := time.Parse("2006-01-02", value)
	return parsed, err == nil
}

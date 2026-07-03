package main

import (
	"fmt"
	"log"
	"path/filepath"
	"strconv"
	"time"

	"albion-market-data/collector/internal/instancelock"
	"albion-market-data/collector/internal/storage/durable"
)

func prepareLocalStorage() (*instancelock.Lock, error) {
	dataRoot := envString("COLLECTOR_DATA_DIR", "./data")
	backupRoot := envString("STORAGE_BACKUP_DIR", "./backups")
	lock, err := instancelock.Acquire(filepath.Join(dataRoot, ".receiver.lock"))
	if err != nil {
		return nil, fmt.Errorf("acquire receiver storage lock: %w", err)
	}
	fail := func(err error) (*instancelock.Lock, error) {
		_ = lock.Close()
		return nil, err
	}

	policy := durable.RetentionPolicy{
		RawDays:        envInt("STORAGE_RAW_RETENTION_DAYS", 30),
		NormalizedDays: envInt("STORAGE_NORMALIZED_RETENTION_DAYS", 365),
		BackupDays:     envInt("STORAGE_BACKUP_RETENTION_DAYS", 30),
		MinimumBackups: envInt("STORAGE_MINIMUM_BACKUPS", 3),
		MaxBytes:       envInt64Value("STORAGE_MAX_BYTES", 10<<30),
	}
	report, err := durable.EnforceRetention(dataRoot, backupRoot, time.Now().UTC(), policy)
	if err != nil {
		return fail(fmt.Errorf("enforce local storage policy: %w", err))
	}
	if report.DeletedFiles > 0 {
		log.Printf("storage retention deleted_files=%d deleted_bytes=%d", report.DeletedFiles, report.DeletedBytes)
	}

	rawRepairs, err := durable.RepairJSONLPatterns(filepath.Join(dataRoot, "raw"), 20<<20, "raw-ingest-*.jsonl")
	if err != nil {
		return fail(fmt.Errorf("repair raw storage: %w", err))
	}
	normalizedRepairs, err := durable.RepairJSONLPatterns(filepath.Join(dataRoot, "normalized"), 20<<20, "market-history-*.jsonl", "market-orders-*.jsonl")
	if err != nil {
		return fail(fmt.Errorf("repair normalized storage: %w", err))
	}
	for _, repair := range append(rawRepairs, normalizedRepairs...) {
		log.Printf("storage JSONL repaired path=%s truncated_bytes=%d quarantine=%s", repair.Path, repair.TruncatedBytes, repair.QuarantinedPath)
	}

	databasePath := envString("LOCAL_DATABASE_PATH", filepath.Join(dataRoot, "database", "market-state.json"))
	outboxPath := envString("UPSTREAM_OUTBOX_PATH", filepath.Join(dataRoot, "outbox", "state.json"))
	for _, path := range []string{databasePath, outboxPath} {
		recovery, err := durable.RecoverAndRefreshJSONBackup(path)
		if err != nil {
			return fail(fmt.Errorf("recover durable JSON %s: %w", path, err))
		}
		if recovery.UsedBackup {
			log.Printf("storage JSON recovered path=%s quarantine=%s", path, recovery.QuarantinedPath)
		}
	}
	return lock, nil
}

func envInt64Value(key string, fallback int64) int64 {
	value := envString(key, "")
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed < 0 {
		return fallback
	}
	return parsed
}

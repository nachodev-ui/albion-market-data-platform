package main

import (
	"path/filepath"
	"testing"
)

func TestStoragePathsFromArgsUsesDataDirectoryOverrides(t *testing.T) {
	t.Setenv("COLLECTOR_DATA_DIR", "env-data")
	t.Setenv("STORAGE_BACKUP_DIR", "env-backups")
	t.Setenv("LOCAL_DATABASE_PATH", "")
	t.Setenv("UPSTREAM_OUTBOX_PATH", "")

	paths := storagePathsFromArgs([]string{"-data-dir", "restored-data"})
	if paths.dataRoot != "restored-data" {
		t.Fatalf("dataRoot=%q", paths.dataRoot)
	}
	if paths.databasePath != filepath.Join("restored-data", "database", "market-state.json") {
		t.Fatalf("databasePath=%q", paths.databasePath)
	}
	if paths.outboxPath != filepath.Join("restored-data", "outbox", "state.json") {
		t.Fatalf("outboxPath=%q", paths.outboxPath)
	}
	if paths.backupRoot != "env-backups" {
		t.Fatalf("backupRoot=%q", paths.backupRoot)
	}
}

func TestStoragePathsFromArgsUsesExplicitStatePaths(t *testing.T) {
	t.Setenv("COLLECTOR_DATA_DIR", "env-data")
	t.Setenv("LOCAL_DATABASE_PATH", "env-database.json")
	t.Setenv("UPSTREAM_OUTBOX_PATH", "env-outbox.json")

	paths := storagePathsFromArgs([]string{
		"--data-dir=flag-data",
		"--database=flag-database.json",
		"--upstream-outbox", "flag-outbox.json",
	})
	if paths.dataRoot != "flag-data" || paths.databasePath != "flag-database.json" || paths.outboxPath != "flag-outbox.json" {
		t.Fatalf("paths=%+v", paths)
	}
}

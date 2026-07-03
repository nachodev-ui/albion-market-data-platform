package durable

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type sample struct {
	Version int    `json:"version"`
	Value   string `json:"value"`
}

func TestAtomicWriteKeepsBackupAndRecoversCorruptPrimary(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "state.json")
	if err := ConfigureBudget(root, 1<<20); err != nil {
		t.Fatal(err)
	}
	first, _ := json.Marshal(sample{1, "one"})
	second, _ := json.Marshal(sample{1, "two"})
	if err := AtomicWrite(path, first, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := AtomicWrite(path, second, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + ".bak"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"version":`), 0o600); err != nil {
		t.Fatal(err)
	}
	got, recovery, err := LoadJSONWithBackup[sample](path, func(v sample) error {
		if v.Version != 1 {
			return errors.New("version")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Value != "one" || !recovery.UsedBackup || recovery.QuarantinedPath == "" {
		t.Fatalf("got=%+v recovery=%+v", got, recovery)
	}
}

func TestRepairJSONLTruncatesOnlyFinalFragment(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	if err := os.WriteFile(path, []byte("{\"id\":1}\n{\"id\":"), 0o600); err != nil {
		t.Fatal(err)
	}
	report, err := RepairJSONL(path, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Repaired || report.QuarantinedPath == "" {
		t.Fatalf("report=%+v", report)
	}
	content, _ := os.ReadFile(path)
	if string(content) != "{\"id\":1}\n" {
		t.Fatalf("content=%q", content)
	}
}

func TestRepairJSONLCompletesValidFinalLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	if err := os.WriteFile(path, []byte("{\"id\":1}"), 0o600); err != nil {
		t.Fatal(err)
	}
	report, err := RepairJSONL(path, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if !report.CompletedFinalLine {
		t.Fatalf("report=%+v", report)
	}
	content, _ := os.ReadFile(path)
	if !strings.HasSuffix(string(content), "\n") {
		t.Fatalf("content=%q", content)
	}
}

func TestBudgetRejectsProjectedGrowth(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "events.jsonl")
	if err := ConfigureBudget(root, 16); err != nil {
		t.Fatal(err)
	}
	if err := AppendJSONLine(path, map[string]int{"id": 1}); err != nil {
		t.Fatal(err)
	}
	err := AppendJSONLine(path, map[string]string{"value": "too large"})
	if !errors.Is(err, ErrStorageLimit) {
		t.Fatalf("err=%v", err)
	}
}

func TestRetentionDeletesOldDataAndKeepsMinimumBackups(t *testing.T) {
	root := t.TempDir()
	backups := filepath.Join(t.TempDir(), "backups")
	os.MkdirAll(filepath.Join(root, "raw"), 0o755)
	os.MkdirAll(filepath.Join(root, "normalized"), 0o755)
	os.MkdirAll(backups, 0o755)
	os.WriteFile(filepath.Join(root, "raw", "raw-ingest-2026-01-01.jsonl"), []byte("x"), 0o600)
	os.WriteFile(filepath.Join(root, "normalized", "market-orders-2026-01-01.jsonl"), []byte("x"), 0o600)
	for i, name := range []string{"a.zip", "b.zip", "c.zip"} {
		p := filepath.Join(backups, name)
		os.WriteFile(p, []byte("x"), 0o600)
		old := time.Date(2026, 1, i+1, 0, 0, 0, 0, time.UTC)
		os.Chtimes(p, old, old)
	}
	report, err := EnforceRetention(root, backups, time.Date(2026, 7, 3, 0, 0, 0, 0, time.UTC), RetentionPolicy{RawDays: 30, NormalizedDays: 30, BackupDays: 30, MinimumBackups: 1, MaxBytes: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	if report.DeletedFiles != 4 {
		t.Fatalf("report=%+v", report)
	}
}

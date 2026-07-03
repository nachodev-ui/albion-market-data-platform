package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPercentileUsesNearestRank(t *testing.T) {
	values := []time.Duration{time.Millisecond, 2 * time.Millisecond, 3 * time.Millisecond, 4 * time.Millisecond, 5 * time.Millisecond}
	if got := percentile(values, 0.50); got != 3*time.Millisecond {
		t.Fatalf("p50=%s", got)
	}
	if got := percentile(values, 0.95); got != 5*time.Millisecond {
		t.Fatalf("p95=%s", got)
	}
}

func TestValidateBudgets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "budgets.json")
	content := `{"schema_version":1,"scenarios":{"example":{"max_p95_ms":20,"max_alloc_bytes_per_op":1024,"min_counters":{"sent":1},"max_counters":{"depth":10}}}}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	value := report{Scenarios: []scenarioSummary{{Name: "example", P95MS: 10, AllocBytesPerOp: 512, Counters: map[string]int64{"sent": 1, "depth": 10}}}}
	if err := validateBudgets(value, path); err != nil {
		t.Fatal(err)
	}
	value.Scenarios[0].P95MS = 25
	if err := validateBudgets(value, path); err == nil {
		t.Fatal("expected budget violation")
	}
}

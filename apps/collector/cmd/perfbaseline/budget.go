package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

type budgetFile struct {
	SchemaVersion int                       `json:"schema_version"`
	Scenarios     map[string]scenarioBudget `json:"scenarios"`
}

type scenarioBudget struct {
	MaxP95MS           float64          `json:"max_p95_ms,omitempty"`
	MaxAllocBytesPerOp float64          `json:"max_alloc_bytes_per_op,omitempty"`
	MaxArtifactsBytes  map[string]int64 `json:"max_artifacts_bytes,omitempty"`
	MinCounters        map[string]int64 `json:"min_counters,omitempty"`
	MaxCounters        map[string]int64 `json:"max_counters,omitempty"`
}

func validateBudgets(value report, path string) error {
	if path == "" {
		return nil
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read performance budgets: %w", err)
	}
	var configured budgetFile
	if err := json.Unmarshal(content, &configured); err != nil {
		return fmt.Errorf("decode performance budgets: %w", err)
	}
	if configured.SchemaVersion != 1 {
		return fmt.Errorf("unsupported performance budget schema version %d", configured.SchemaVersion)
	}
	actual := make(map[string]scenarioSummary, len(value.Scenarios))
	for _, scenario := range value.Scenarios {
		actual[scenario.Name] = scenario
	}
	var violations []string
	for name, budget := range configured.Scenarios {
		scenario, exists := actual[name]
		if !exists {
			violations = append(violations, name+": required scenario missing")
			continue
		}
		if budget.MaxP95MS > 0 && scenario.P95MS > budget.MaxP95MS {
			violations = append(violations, fmt.Sprintf("%s: p95 %.3f ms > %.3f ms", name, scenario.P95MS, budget.MaxP95MS))
		}
		if budget.MaxAllocBytesPerOp > 0 && scenario.AllocBytesPerOp > budget.MaxAllocBytesPerOp {
			violations = append(violations, fmt.Sprintf("%s: allocations %.0f B/op > %.0f B/op", name, scenario.AllocBytesPerOp, budget.MaxAllocBytesPerOp))
		}
		for key, maximum := range budget.MaxArtifactsBytes {
			if scenario.ArtifactsBytes[key] > maximum {
				violations = append(violations, fmt.Sprintf("%s: artifact %s=%d > %d bytes", name, key, scenario.ArtifactsBytes[key], maximum))
			}
		}
		for key, minimum := range budget.MinCounters {
			if scenario.Counters[key] < minimum {
				violations = append(violations, fmt.Sprintf("%s: counter %s=%d < %d", name, key, scenario.Counters[key], minimum))
			}
		}
		for key, maximum := range budget.MaxCounters {
			if scenario.Counters[key] > maximum {
				violations = append(violations, fmt.Sprintf("%s: counter %s=%d > %d", name, key, scenario.Counters[key], maximum))
			}
		}
	}
	if len(violations) == 0 {
		fmt.Printf("Budgets=%s status=pass\n", path)
		return nil
	}
	sort.Strings(violations)
	return fmt.Errorf("performance budget violations:\n- %s", strings.Join(violations, "\n- "))
}

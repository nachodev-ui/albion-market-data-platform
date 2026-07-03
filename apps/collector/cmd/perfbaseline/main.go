package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

var (
	outputPath  = flag.String("output", "", "JSON output path; stdout when empty")
	budgetsPath = flag.String("budgets", "", "optional performance budget JSON to validate")
	profilesDir = flag.String("profiles-dir", "", "optional directory for CPU, heap and mutex profiles")
	sampleCount = flag.Int("samples", 25, "samples per scenario")
)

func main() {
	flag.Parse()
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	if *sampleCount < 3 {
		return fmt.Errorf("samples must be at least 3")
	}
	stopProfiles, err := startProfiles(strings.TrimSpace(*profilesDir))
	if err != nil {
		return err
	}
	defer stopProfiles()

	root, err := os.MkdirTemp("", "albion-receiver-perfbaseline-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(root)

	result := report{Mode: "local", GeneratedAt: time.Now().UTC(), GoVersion: runtime.Version(), GOOS: runtime.GOOS, GOARCH: runtime.GOARCH, CPU: runtime.NumCPU()}
	if err := runLocalScenarios(&result, root, *sampleCount); err != nil {
		return err
	}
	result.Skipped = append(result.Skipped, skippedScenario{Name: "postgres_round_trip_prices", Reason: "measured by the PostgreSQL integration job"})

	encoded, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	if strings.TrimSpace(*outputPath) == "" {
		_, _ = os.Stdout.Write(encoded)
	} else {
		if err := os.MkdirAll(filepath.Dir(*outputPath), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(*outputPath, encoded, 0o600); err != nil {
			return err
		}
		fmt.Printf("Baseline=%s\n", *outputPath)
	}
	return validateBudgets(result, strings.TrimSpace(*budgetsPath))
}

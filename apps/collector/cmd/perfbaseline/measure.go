package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"sort"
	"time"
)

func addMeasured(target *report, name string, samples int, fn sampleFn) error {
	summary, err := measure(name, samples, fn)
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	target.Scenarios = append(target.Scenarios, summary)
	fmt.Printf("%-42s p50=%9.3f ms p95=%9.3f ms alloc=%10.0f B/op\n", name, summary.P50MS, summary.P95MS, summary.AllocBytesPerOp)
	return nil
}

func measure(name string, samples int, fn sampleFn) (scenarioSummary, error) {
	durations := make([]time.Duration, 0, samples)
	var totalAlloc, totalMallocs uint64
	artifacts := map[string]int64{}
	counters := map[string]int64{}
	for sample := 0; sample < samples; sample++ {
		runtime.GC()
		var before runtime.MemStats
		runtime.ReadMemStats(&before)
		started := time.Now()
		details, err := fn()
		duration := time.Since(started)
		var after runtime.MemStats
		runtime.ReadMemStats(&after)
		if err != nil {
			return scenarioSummary{}, err
		}
		durations = append(durations, duration)
		totalAlloc += after.TotalAlloc - before.TotalAlloc
		totalMallocs += after.Mallocs - before.Mallocs
		for key, value := range details.Artifacts {
			if value > artifacts[key] {
				artifacts[key] = value
			}
		}
		for key, value := range details.Counters {
			if value > counters[key] {
				counters[key] = value
			}
		}
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	var total time.Duration
	for _, duration := range durations {
		total += duration
	}
	result := scenarioSummary{
		Name: name, Samples: samples, TotalMS: milliseconds(total), MeanMS: milliseconds(total / time.Duration(samples)),
		P50MS: milliseconds(percentile(durations, 0.50)), P95MS: milliseconds(percentile(durations, 0.95)),
		MinMS: milliseconds(durations[0]), MaxMS: milliseconds(durations[len(durations)-1]),
		AllocBytesPerOp: float64(totalAlloc) / float64(samples), AllocsPerOp: float64(totalMallocs) / float64(samples),
	}
	if len(artifacts) > 0 {
		result.ArtifactsBytes = artifacts
	}
	if len(counters) > 0 {
		result.Counters = counters
	}
	return result, nil
}

func startProfiles(directory string) (func(), error) {
	if directory == "" {
		return func() {}, nil
	}
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return nil, err
	}
	cpuFile, err := os.Create(filepath.Join(directory, "cpu.pprof"))
	if err != nil {
		return nil, err
	}
	if err := pprof.StartCPUProfile(cpuFile); err != nil {
		cpuFile.Close()
		return nil, err
	}
	runtime.SetMutexProfileFraction(1)
	return func() {
		pprof.StopCPUProfile()
		_ = cpuFile.Close()
		runtime.GC()
		if file, err := os.Create(filepath.Join(directory, "heap.pprof")); err == nil {
			_ = pprof.WriteHeapProfile(file)
			_ = file.Close()
		}
		if file, err := os.Create(filepath.Join(directory, "mutex.pprof")); err == nil {
			_ = pprof.Lookup("mutex").WriteTo(file, 0)
			_ = file.Close()
		}
	}, nil
}

func percentile(sorted []time.Duration, value float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	index := int(float64(len(sorted))*value+0.999999999) - 1
	if index < 0 {
		index = 0
	}
	if index >= len(sorted) {
		index = len(sorted) - 1
	}
	return sorted[index]
}
func milliseconds(value time.Duration) float64 { return float64(value) / float64(time.Millisecond) }
func fileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}
func boolInt(value bool) int64 {
	if value {
		return 1
	}
	return 0
}
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

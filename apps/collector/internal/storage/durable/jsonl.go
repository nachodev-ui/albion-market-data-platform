package durable

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"
)

type JSONLRepairReport struct {
	Path               string
	Repaired           bool
	CompletedFinalLine bool
	TruncatedBytes     int64
	QuarantinedPath    string
}

func RepairJSONL(path string, maxLineBytes int) (JSONLRepairReport, error) {
	report := JSONLRepairReport{Path: path}
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if errors.Is(err, os.ErrNotExist) {
		return report, nil
	}
	if err != nil {
		return report, fmt.Errorf("open JSONL %s: %w", path, err)
	}
	defer file.Close()
	if maxLineBytes <= 0 {
		maxLineBytes = 20 << 20
	}
	reader := bufio.NewReaderSize(file, 64*1024)
	offset := int64(0)
	lineNumber := 0
	for {
		line, readErr := reader.ReadBytes('\n')
		if len(line) > maxLineBytes {
			return report, fmt.Errorf("JSONL %s line %d exceeds %d bytes", path, lineNumber+1, maxLineBytes)
		}
		if len(line) > 0 {
			lineNumber++
			hasNewline := line[len(line)-1] == '\n'
			payload := bytes.TrimSpace(line)
			if len(payload) == 0 {
				if readErr == io.EOF && !hasNewline {
					if err := file.Truncate(offset); err != nil {
						return report, err
					}
					if err := file.Sync(); err != nil {
						return report, err
					}
					report.Repaired = true
					report.TruncatedBytes = int64(len(line))
				} else {
					offset += int64(len(line))
				}
			} else if json.Valid(payload) {
				offset += int64(len(line))
				if readErr == io.EOF && !hasNewline {
					if _, err := file.Seek(0, io.SeekEnd); err != nil {
						return report, err
					}
					if _, err := file.Write([]byte{'\n'}); err != nil {
						return report, err
					}
					if err := file.Sync(); err != nil {
						return report, err
					}
					report.Repaired = true
					report.CompletedFinalLine = true
				}
			} else if readErr == io.EOF && !hasNewline {
				quarantine := fmt.Sprintf("%s.truncated-%s", path, time.Now().UTC().Format("20060102T150405.000000000Z"))
				if err := os.WriteFile(quarantine, line, 0o600); err != nil {
					return report, fmt.Errorf("quarantine JSONL fragment: %w", err)
				}
				if err := file.Truncate(offset); err != nil {
					return report, fmt.Errorf("truncate JSONL %s: %w", path, err)
				}
				if err := file.Sync(); err != nil {
					return report, fmt.Errorf("sync repaired JSONL %s: %w", path, err)
				}
				report.Repaired = true
				report.TruncatedBytes = int64(len(line))
				report.QuarantinedPath = quarantine
			} else {
				return report, fmt.Errorf("JSONL %s line %d is corrupt", path, lineNumber)
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return report, fmt.Errorf("read JSONL %s: %w", path, readErr)
		}
	}
	return report, nil
}

func RepairJSONLPatterns(directory string, maxLineBytes int, patterns ...string) ([]JSONLRepairReport, error) {
	paths := make([]string, 0)
	for _, pattern := range patterns {
		matches, err := filepath.Glob(filepath.Join(directory, pattern))
		if err != nil {
			return nil, err
		}
		paths = append(paths, matches...)
	}
	sort.Strings(paths)
	reports := make([]JSONLRepairReport, 0)
	for _, path := range paths {
		report, err := RepairJSONL(path, maxLineBytes)
		if err != nil {
			return nil, err
		}
		if report.Repaired {
			reports = append(reports, report)
		}
	}
	return reports, nil
}

func AppendJSONLine(path string, value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode JSONL record: %w", err)
	}
	line := append(encoded, '\n')
	budgetMu.Lock()
	defer budgetMu.Unlock()
	if err := checkAppendLocked(path, int64(len(line))); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	if _, err := file.Write(line); err != nil {
		file.Close()
		return fmt.Errorf("append %s: %w", path, err)
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return fmt.Errorf("sync %s: %w", path, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close %s: %w", path, err)
	}
	return nil
}

package observability

import (
	"bytes"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestLoggerKeepsFieldOrderWithoutColor(t *testing.T) {
	var output bytes.Buffer
	logger := NewLogger(&output, "never")
	logger.now = func() time.Time { return time.Date(2026, 6, 26, 1, 2, 3, 0, time.UTC) }

	logger.Event(LevelOK, "upstream.batch_sent",
		F("request_id", "abc"),
		F("entries", 500),
		F("duration_ms", 12.5),
	)

	line := output.String()
	if strings.Contains(line, "\x1b[") {
		t.Fatalf("unexpected ANSI sequence: %q", line)
	}
	requestIndex := strings.Index(line, `request_id="abc"`)
	entriesIndex := strings.Index(line, "entries=500")
	durationIndex := strings.Index(line, "duration_ms=12.5")
	if requestIndex < 0 || entriesIndex <= requestIndex || durationIndex <= entriesIndex {
		t.Fatalf("fields are not ordered: %q", line)
	}
}

func TestLoggerForcedColor(t *testing.T) {
	var output bytes.Buffer
	logger := NewLogger(&output, "always")
	logger.Event(LevelDrop, "upstream.queue_drop")
	if !strings.Contains(output.String(), "\x1b[31m") {
		t.Fatalf("expected red ANSI sequence: %q", output.String())
	}
}

func TestLoggerConcurrentWritesAreCompleteLines(t *testing.T) {
	var output bytes.Buffer
	logger := NewLogger(&output, "never")

	const writers = 50
	var wait sync.WaitGroup
	wait.Add(writers)
	for index := 0; index < writers; index++ {
		go func(value int) {
			defer wait.Done()
			logger.Event(LevelInfo, "concurrent.event", F("value", value))
		}(index)
	}
	wait.Wait()

	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != writers {
		t.Fatalf("lines = %d, want %d", len(lines), writers)
	}
	for _, line := range lines {
		if !strings.Contains(line, "[INFO ] concurrent.event value=") {
			t.Fatalf("partial or malformed line: %q", line)
		}
	}
}

func TestLoggerRedactsSensitiveFields(t *testing.T) {
	var output bytes.Buffer
	logger := NewLogger(&output, "never")
	logger.Event(LevelInfo, "security.test",
		F("upstream_token", "super-secret-value"),
		F("authorization", "Bearer secret"),
		F("credential_source", "file"),
	)

	line := output.String()
	if strings.Contains(line, "super-secret-value") || strings.Contains(line, "Bearer secret") {
		t.Fatalf("sensitive value leaked: %q", line)
	}
	if strings.Count(line, `"[REDACTED]"`) != 2 {
		t.Fatalf("redactions = %q, want two", line)
	}
	if !strings.Contains(line, `credential_source="file"`) {
		t.Fatalf("safe metadata missing: %q", line)
	}
}

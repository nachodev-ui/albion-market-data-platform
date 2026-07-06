package observability

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
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

func TestJSONLoggerDisablesColorAndEmitsStructuredRecord(t *testing.T) {
	var output bytes.Buffer
	logger := NewLoggerWithOptions(&output, LoggerOptions{ColorMode: "always", Format: "json"})
	logger.now = func() time.Time { return time.Date(2026, 7, 6, 1, 2, 3, 0, time.UTC) }

	logger.Event(LevelWarn, "ingest.orders_invalid",
		F("request_id", "req-12345678"),
		F("headers", http.Header{
			"Authorization": []string{"Bearer secret-token"},
			"User-Agent":    []string{"aodp-test"},
		}),
		F("error", errors.New("invalid json")),
	)

	if logger.ColorEnabled() {
		t.Fatal("json logging must disable ANSI colors")
	}
	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatalf("decode json log: %v\n%s", err, output.String())
	}
	if record["ts"] != "2026-07-06T01:02:03Z" || record["level"] != "WARN" || record["event"] != "ingest.orders_invalid" {
		t.Fatalf("unexpected record metadata: %#v", record)
	}
	if record["request_id"] != "req-12345678" {
		t.Fatalf("missing request_id: %#v", record)
	}
	if record["error_category"] != string(ErrorCategoryInternal) {
		t.Fatalf("error_category=%#v", record["error_category"])
	}
	headers, ok := record["headers"].(map[string]any)
	if !ok {
		t.Fatalf("headers not encoded as object: %#v", record["headers"])
	}
	if headers["Authorization"] != redactedValue {
		t.Fatalf("authorization leaked or was not redacted: %#v", headers["Authorization"])
	}
	if strings.Contains(output.String(), "secret-token") {
		t.Fatalf("secret leaked: %s", output.String())
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
		F("headers", http.Header{
			"Cookie":     []string{"session=secret"},
			"User-Agent": []string{"aodp-test"},
		}),
		F("credential_source", "file"),
	)

	line := output.String()
	if strings.Contains(line, "super-secret-value") || strings.Contains(line, "Bearer secret") || strings.Contains(line, "session=secret") {
		t.Fatalf("sensitive value leaked: %q", line)
	}
	if strings.Count(line, `"[REDACTED]"`) < 3 {
		t.Fatalf("redactions = %q, want at least three", line)
	}
	if !strings.Contains(line, `credential_source="file"`) {
		t.Fatalf("safe metadata missing: %q", line)
	}
}

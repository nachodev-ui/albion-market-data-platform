package observability

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"
)

const redactedValue = "[REDACTED]"

type Level string

const (
	LevelInfo  Level = "INFO"
	LevelOK    Level = "OK"
	LevelWarn  Level = "WARN"
	LevelError Level = "ERROR"
	LevelRetry Level = "RETRY"
	LevelDrop  Level = "DROP"
)

type LogFormat string

const (
	LogFormatText LogFormat = "text"
	LogFormatJSON LogFormat = "json"
)

type LoggerOptions struct {
	ColorMode string
	Format    string
}

type Field struct {
	Key   string
	Value any
}

func F(key string, value any) Field {
	return Field{Key: key, Value: value}
}

type Logger struct {
	writer io.Writer
	color  bool
	format LogFormat
	mu     sync.Mutex
	now    func() time.Time
}

func NewLogger(writer io.Writer, colorMode string) *Logger {
	return NewLoggerWithOptions(writer, LoggerOptions{ColorMode: colorMode, Format: string(LogFormatText)})
}

func NewLoggerWithOptions(writer io.Writer, options LoggerOptions) *Logger {
	if writer == nil {
		writer = os.Stdout
	}
	format := parseLogFormat(options.Format)
	mode := strings.ToLower(strings.TrimSpace(options.ColorMode))
	color := false
	if format == LogFormatText {
		switch mode {
		case "always":
			color = true
		case "never":
			color = false
		default:
			color = supportsColor(writer)
		}
	}
	return &Logger{writer: writer, color: color, format: format, now: time.Now}
}

func parseLogFormat(value string) LogFormat {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "json":
		return LogFormatJSON
	default:
		return LogFormatText
	}
}

func (l *Logger) ColorEnabled() bool {
	return l != nil && l.color
}

func (l *Logger) Event(level Level, event string, fields ...Field) {
	if l == nil {
		return
	}

	timestamp := l.now().UTC()
	event = strings.TrimSpace(event)
	fields = normalizeFields(fields)
	fields = appendImplicitErrorCategory(fields)
	fields = appendContextRequestID(context.Background(), fields)

	var line string
	if l.format == LogFormatJSON {
		line = l.jsonLine(timestamp, level, event, fields)
	} else {
		line = l.textLine(timestamp, level, event, fields)
	}

	l.mu.Lock()
	_, _ = io.WriteString(l.writer, line)
	l.mu.Unlock()
}

// Printf mantiene compatibilidad con componentes antiguos y los presenta como
// eventos INFO estructurados, sin perder el texto original.
func (l *Logger) Printf(format string, args ...any) {
	l.Event(LevelInfo, "receiver.message", F("message", fmt.Sprintf(format, args...)))
}

func (l *Logger) textLine(timestamp time.Time, level Level, event string, fields []Field) string {
	var line strings.Builder
	line.WriteString(timestamp.Format(time.RFC3339Nano))
	line.WriteString(" [")
	line.WriteString(l.formatLevel(level))
	line.WriteString("] ")
	line.WriteString(event)
	for _, field := range fields {
		key := strings.TrimSpace(field.Key)
		if key == "" {
			continue
		}
		line.WriteByte(' ')
		line.WriteString(key)
		line.WriteByte('=')
		line.WriteString(formatField(key, field.Value))
	}
	line.WriteByte('\n')
	return line.String()
}

func (l *Logger) jsonLine(timestamp time.Time, level Level, event string, fields []Field) string {
	record := make(map[string]any, len(fields)+3)
	record["ts"] = timestamp.Format(time.RFC3339Nano)
	record["level"] = strings.ToUpper(strings.TrimSpace(string(level)))
	record["event"] = event
	for _, field := range fields {
		key := strings.TrimSpace(field.Key)
		if key == "" {
			continue
		}
		record[key] = sanitizeFieldValue(key, field.Value)
	}
	encoded, err := json.Marshal(record)
	if err != nil {
		return l.textLine(timestamp, level, event, fields)
	}
	return string(encoded) + "\n"
}

func normalizeFields(fields []Field) []Field {
	normalized := make([]Field, 0, len(fields))
	seen := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		key := strings.TrimSpace(field.Key)
		if key == "" {
			continue
		}
		if _, exists := seen[key]; exists {
			normalized = append(normalized, Field{Key: key, Value: field.Value})
			continue
		}
		seen[key] = struct{}{}
		normalized = append(normalized, Field{Key: key, Value: field.Value})
	}
	return normalized
}

func appendImplicitErrorCategory(fields []Field) []Field {
	hasError := false
	hasCategory := false
	var errorValue any
	for _, field := range fields {
		key := strings.ToLower(strings.TrimSpace(field.Key))
		switch key {
		case "error":
			hasError = true
			errorValue = field.Value
		case "error_category":
			hasCategory = true
		}
	}
	if !hasError || hasCategory {
		return fields
	}
	return append(fields, F("error_category", ErrorCategoryForValue(errorValue)))
}

func appendContextRequestID(ctx context.Context, fields []Field) []Field {
	if requestID := RequestIDFromContext(ctx); requestID != "" && !hasField(fields, "request_id") {
		return append(fields, F("request_id", requestID))
	}
	return fields
}

func AppendRequestIDField(ctx context.Context, fields ...Field) []Field {
	fields = normalizeFields(fields)
	return appendContextRequestID(ctx, fields)
}

func hasField(fields []Field, key string) bool {
	for _, field := range fields {
		if strings.EqualFold(strings.TrimSpace(field.Key), key) {
			return true
		}
	}
	return false
}

func (l *Logger) formatLevel(level Level) string {
	label := fmt.Sprintf("%-5s", strings.ToUpper(strings.TrimSpace(string(level))))
	if !l.color {
		return label
	}
	code := "36"
	switch level {
	case LevelOK:
		code = "32"
	case LevelWarn, LevelRetry:
		code = "33"
	case LevelError, LevelDrop:
		code = "31"
	case LevelInfo:
		code = "36"
	}
	return "\x1b[" + code + "m" + label + "\x1b[0m"
}

func formatField(key string, value any) string {
	return formatValue(sanitizeFieldValue(key, value))
}

func sanitizeFieldValue(key string, value any) any {
	if isSensitiveKey(key) {
		return redactedValue
	}
	return sanitizeValue(value, 0)
}

func isSensitiveKey(key string) bool {
	normalized := normalizeSensitiveKey(key)
	if normalized == "" || normalized == "credential_source" {
		return false
	}
	switch normalized {
	case "authorization", "proxy_authorization", "cookie", "set_cookie",
		"database_url", "dsn", "connection_string",
		"password", "secret", "token",
		"api_key", "apikey", "access_token", "refresh_token", "id_token",
		"bearer_token", "client_secret", "private_key",
		"session", "session_id", "csrf_token":
		return true
	}
	return strings.HasSuffix(normalized, "_token") ||
		strings.HasSuffix(normalized, "_secret") ||
		strings.HasSuffix(normalized, "_password") ||
		strings.HasSuffix(normalized, "_api_key") ||
		strings.HasSuffix(normalized, "_cookie") ||
		strings.HasSuffix(normalized, "_session")
}

func normalizeSensitiveKey(key string) string {
	key = strings.ToLower(strings.TrimSpace(key))
	key = strings.ReplaceAll(key, "-", "_")
	key = strings.ReplaceAll(key, " ", "_")
	return key
}

func sanitizeValue(value any, depth int) any {
	if depth > 6 {
		return fmt.Sprint(value)
	}
	switch typed := value.(type) {
	case nil:
		return nil
	case error:
		return sanitizeErrorMessage(typed)
	case time.Time:
		if typed.IsZero() {
			return nil
		}
		return typed.UTC().Format(time.RFC3339Nano)
	case *time.Time:
		if typed == nil || typed.IsZero() {
			return nil
		}
		return typed.UTC().Format(time.RFC3339Nano)
	case time.Duration:
		return typed.String()
	case fmt.Stringer:
		return typed.String()
	}

	reflected := reflect.ValueOf(value)
	if !reflected.IsValid() {
		return nil
	}
	switch reflected.Kind() {
	case reflect.Pointer, reflect.Interface:
		if reflected.IsNil() {
			return nil
		}
		return sanitizeValue(reflected.Elem().Interface(), depth+1)
	case reflect.Map:
		result := make(map[string]any, reflected.Len())
		for _, mapKey := range reflected.MapKeys() {
			key := fmt.Sprint(mapKey.Interface())
			mapValue := reflected.MapIndex(mapKey)
			if isSensitiveKey(key) {
				result[key] = redactedValue
				continue
			}
			if mapValue.IsValid() && mapValue.CanInterface() {
				result[key] = sanitizeValue(mapValue.Interface(), depth+1)
			}
		}
		return result
	case reflect.Slice, reflect.Array:
		length := reflected.Len()
		result := make([]any, 0, length)
		for index := 0; index < length; index++ {
			item := reflected.Index(index)
			if item.IsValid() && item.CanInterface() {
				result = append(result, sanitizeValue(item.Interface(), depth+1))
			}
		}
		return result
	default:
		return value
	}
}

func sanitizeErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	value := strings.Join(strings.Fields(err.Error()), " ")
	if len(value) > maxRegistryErrorLength {
		value = value[:maxRegistryErrorLength] + "..."
	}
	return value
}

func formatValue(value any) string {
	switch typed := value.(type) {
	case nil:
		return "null"
	case string:
		return strconv.Quote(typed)
	case []byte:
		return strconv.Quote(string(typed))
	case time.Time:
		if typed.IsZero() {
			return "null"
		}
		return strconv.Quote(typed.UTC().Format(time.RFC3339Nano))
	case *time.Time:
		if typed == nil || typed.IsZero() {
			return "null"
		}
		return strconv.Quote(typed.UTC().Format(time.RFC3339Nano))
	case time.Duration:
		return strconv.Quote(typed.String())
	case fmt.Stringer:
		return strconv.Quote(typed.String())
	}

	reflected := reflect.ValueOf(value)
	if reflected.IsValid() {
		switch reflected.Kind() {
		case reflect.Map, reflect.Slice, reflect.Array:
			if encoded, err := json.Marshal(value); err == nil {
				return string(encoded)
			}
		}
	}
	return fmt.Sprint(value)
}

func ErrorCategoryForValue(value any) ErrorCategory {
	switch typed := value.(type) {
	case nil:
		return ErrorCategoryNone
	case ErrorCategory:
		return typed
	case string:
		return ErrorCategoryInternal
	case error:
		return ErrorCategoryForError(typed)
	default:
		if err, ok := value.(interface{ Error() string }); ok {
			return ErrorCategoryForError(errors.New(err.Error()))
		}
		return ErrorCategoryInternal
	}
}

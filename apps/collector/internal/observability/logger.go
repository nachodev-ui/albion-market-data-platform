package observability

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Level string

const (
	LevelInfo  Level = "INFO"
	LevelOK    Level = "OK"
	LevelWarn  Level = "WARN"
	LevelError Level = "ERROR"
	LevelRetry Level = "RETRY"
	LevelDrop  Level = "DROP"
)

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
	mu     sync.Mutex
	now    func() time.Time
}

func NewLogger(writer io.Writer, colorMode string) *Logger {
	if writer == nil {
		writer = os.Stdout
	}
	mode := strings.ToLower(strings.TrimSpace(colorMode))
	color := false
	switch mode {
	case "always":
		color = true
	case "never":
		color = false
	default:
		color = supportsColor(writer)
	}
	return &Logger{writer: writer, color: color, now: time.Now}
}

func (l *Logger) ColorEnabled() bool {
	return l != nil && l.color
}

func (l *Logger) Event(level Level, event string, fields ...Field) {
	if l == nil {
		return
	}

	var line strings.Builder
	line.WriteString(l.now().UTC().Format(time.RFC3339Nano))
	line.WriteString(" [")
	line.WriteString(l.formatLevel(level))
	line.WriteString("] ")
	line.WriteString(strings.TrimSpace(event))
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

	l.mu.Lock()
	_, _ = io.WriteString(l.writer, line.String())
	l.mu.Unlock()
}

// Printf mantiene compatibilidad con componentes antiguos y los presenta como
// eventos INFO estructurados, sin perder el texto original.
func (l *Logger) Printf(format string, args ...any) {
	l.Event(LevelInfo, "receiver.message", F("message", fmt.Sprintf(format, args...)))
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
	if isSensitiveKey(key) {
		return strconv.Quote("[REDACTED]")
	}
	return formatValue(value)
}

func isSensitiveKey(key string) bool {
	normalized := strings.ToLower(strings.TrimSpace(key))
	switch normalized {
	case "authorization", "proxy_authorization", "database_url", "dsn", "connection_string", "password", "secret", "token":
		return true
	}
	return strings.HasSuffix(normalized, "_token") ||
		strings.HasSuffix(normalized, "_secret") ||
		strings.HasSuffix(normalized, "_password")
}

func formatValue(value any) string {
	switch typed := value.(type) {
	case nil:
		return "null"
	case string:
		return strconv.Quote(typed)
	case []byte:
		return strconv.Quote(string(typed))
	case error:
		return strconv.Quote(typed.Error())
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
	default:
		return fmt.Sprint(value)
	}
}

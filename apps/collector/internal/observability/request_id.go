package observability

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const HeaderRequestID = "X-Request-ID"

type requestIDContextKey struct{}

type httpEventLogger interface {
	Event(Level, string, ...Field)
}

type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (r *statusRecorder) WriteHeader(status int) {
	if r.status == 0 {
		r.status = status
	}
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(payload []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	written, err := r.ResponseWriter.Write(payload)
	r.bytes += written
	return written, err
}

func WithRequestID(next http.Handler) http.Handler {
	if next == nil {
		next = http.NotFoundHandler()
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := RequestIDFromContext(r.Context())
		if requestID == "" {
			requestID = CanonicalRequestID(r.Header.Get(HeaderRequestID))
		}
		if requestID == "" {
			requestID = NewRequestID()
		}
		w.Header().Set(HeaderRequestID, requestID)
		ctx := context.WithValue(r.Context(), requestIDContextKey{}, requestID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func WithHTTPLogging(next http.Handler, logger httpEventLogger) http.Handler {
	if next == nil {
		next = http.NotFoundHandler()
	}
	if logger == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		startedAt := time.Now()
		recorder := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(recorder, r)
		status := recorder.status
		if status == 0 {
			status = http.StatusOK
		}
		level := LevelInfo
		if status >= http.StatusInternalServerError {
			level = LevelError
		} else if status >= http.StatusBadRequest {
			level = LevelWarn
		}
		fields := []Field{
			F("request_id", RequestIDFromContext(r.Context())),
			F("method", r.Method),
			F("path", r.URL.Path),
			F("status", status),
			F("duration_ms", float64(time.Since(startedAt))/float64(time.Millisecond)),
			F("response_bytes", recorder.bytes),
		}
		if status >= http.StatusBadRequest {
			fields = append(fields, F("error_category", ErrorCategoryForHTTPStatus(status)))
		}
		logger.Event(level, "http.request_completed", fields...)
	})
}

func RequestIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	value, _ := ctx.Value(requestIDContextKey{}).(string)
	return CanonicalRequestID(value)
}

func ContextWithRequestID(ctx context.Context, requestID string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	requestID = CanonicalRequestID(requestID)
	if requestID == "" {
		requestID = NewRequestID()
	}
	return context.WithValue(ctx, requestIDContextKey{}, requestID)
}

func CanonicalRequestID(value string) string {
	value = strings.TrimSpace(value)
	if len(value) < 8 || len(value) > 128 {
		return ""
	}
	for _, char := range value {
		if (char >= 'a' && char <= 'z') ||
			(char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') ||
			char == '-' || char == '_' || char == '.' || char == ':' {
			continue
		}
		return ""
	}
	return value
}

func NewRequestID() string {
	var random [16]byte
	if _, err := rand.Read(random[:]); err == nil {
		return hex.EncodeToString(random[:])
	}
	return fmt.Sprintf("%x", time.Now().UTC().UnixNano())
}

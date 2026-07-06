package observability

import (
	"context"
	"errors"
	"net"
	"net/http"
)

type ErrorCategory string

const (
	ErrorCategoryNone              ErrorCategory = "none"
	ErrorCategoryInvalidRequest    ErrorCategory = "invalid_request"
	ErrorCategoryPayloadDecode     ErrorCategory = "payload_decode"
	ErrorCategoryNormalization     ErrorCategory = "normalization"
	ErrorCategoryStorage           ErrorCategory = "storage"
	ErrorCategoryBackpressure      ErrorCategory = "backpressure"
	ErrorCategoryForwarderQueue    ErrorCategory = "forwarder_queue"
	ErrorCategoryUpstreamPayload   ErrorCategory = "upstream_payload"
	ErrorCategoryUpstreamHTTP      ErrorCategory = "upstream_http"
	ErrorCategoryUpstreamTransport ErrorCategory = "upstream_transport"
	ErrorCategoryUpstreamResponse  ErrorCategory = "upstream_response"
	ErrorCategoryTimeout           ErrorCategory = "timeout"
	ErrorCategoryCanceled          ErrorCategory = "canceled"
	ErrorCategoryInternal          ErrorCategory = "internal"
)

type categorizedError interface {
	ErrorCategory() string
}

func ErrorFields(category ErrorCategory, err error) []Field {
	fields := []Field{F("error_category", category)}
	if err != nil {
		fields = append(fields, F("error", err))
	}
	return fields
}

func ErrorCategoryForError(err error) ErrorCategory {
	if err == nil {
		return ErrorCategoryNone
	}
	var categorized categorizedError
	if errors.As(err, &categorized) {
		if category := ParseErrorCategory(categorized.ErrorCategory()); category != "" {
			return category
		}
	}
	if errors.Is(err, context.Canceled) {
		return ErrorCategoryCanceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return ErrorCategoryTimeout
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		if netErr.Timeout() {
			return ErrorCategoryTimeout
		}
		return ErrorCategoryUpstreamTransport
	}
	return ErrorCategoryInternal
}

func ParseErrorCategory(value string) ErrorCategory {
	switch ErrorCategory(value) {
	case ErrorCategoryInvalidRequest,
		ErrorCategoryPayloadDecode,
		ErrorCategoryNormalization,
		ErrorCategoryStorage,
		ErrorCategoryBackpressure,
		ErrorCategoryForwarderQueue,
		ErrorCategoryUpstreamPayload,
		ErrorCategoryUpstreamHTTP,
		ErrorCategoryUpstreamTransport,
		ErrorCategoryUpstreamResponse,
		ErrorCategoryTimeout,
		ErrorCategoryCanceled,
		ErrorCategoryInternal:
		return ErrorCategory(value)
	default:
		return ""
	}
}

func ErrorCategoryForHTTPStatus(statusCode int) ErrorCategory {
	if statusCode == 0 {
		return ErrorCategoryUpstreamTransport
	}
	if statusCode == http.StatusRequestTimeout || statusCode == http.StatusGatewayTimeout {
		return ErrorCategoryTimeout
	}
	if statusCode >= http.StatusInternalServerError {
		return ErrorCategoryUpstreamHTTP
	}
	if statusCode >= http.StatusBadRequest {
		return ErrorCategoryInvalidRequest
	}
	return ErrorCategoryNone
}

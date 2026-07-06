package upstream

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"albion-market-data/collector/internal/observability"
)

const (
	maxErrorBodyBytes   = 4096
	maxSuccessBodyBytes = 64 << 10
)

type ClientOptions struct {
	BaseURL      string
	Token        string
	Timeout      time.Duration
	UseGzip      bool
	RequireHTTPS bool
}

type Client struct {
	baseURL string
	token   string
	useGzip bool
	client  *http.Client
}

type SendError struct {
	StatusCode int
	Duration   time.Duration
	Category   observability.ErrorCategory
	Message    string
	Cause      error
}

func (e *SendError) Error() string {
	if e == nil {
		return ""
	}
	if strings.TrimSpace(e.Message) != "" {
		return e.Message
	}
	if e.Cause != nil {
		return e.Cause.Error()
	}
	return "upstream request failed"
}

func (e *SendError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func (e *SendError) ErrorCategory() string {
	if e == nil || e.Category == "" {
		return string(observability.ErrorCategoryInternal)
	}
	return string(e.Category)
}

func SendErrorDetails(err error) (statusCode int, duration time.Duration) {
	var sendErr *SendError
	if errors.As(err, &sendErr) {
		return sendErr.StatusCode, sendErr.Duration
	}
	return 0, 0
}

func SendErrorCategory(err error) observability.ErrorCategory {
	var sendErr *SendError
	if errors.As(err, &sendErr) && sendErr.Category != "" {
		return sendErr.Category
	}
	return observability.ErrorCategoryForError(err)
}

func NewClient(baseURL, token string, timeout time.Duration, useGzip bool) (*Client, error) {
	return NewClientWithOptions(ClientOptions{
		BaseURL: baseURL,
		Token:   token,
		Timeout: timeout,
		UseGzip: useGzip,
	})
}

func NewClientWithOptions(options ClientOptions) (*Client, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(options.BaseURL), "/")
	if baseURL == "" {
		return nil, fmt.Errorf("base URL is required")
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Host == "" {
		return nil, fmt.Errorf("base URL must be an absolute http or https URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("base URL scheme must be http or https")
	}
	if parsed.User != nil {
		return nil, fmt.Errorf("base URL must not contain user credentials")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("base URL must not contain a query string or fragment")
	}
	if options.RequireHTTPS && parsed.Scheme != "https" {
		return nil, fmt.Errorf("HTTPS is required for the upstream API")
	}
	if options.Timeout <= 0 {
		options.Timeout = 5 * time.Second
	}
	return &Client{
		baseURL: baseURL,
		token:   strings.TrimSpace(options.Token),
		useGzip: options.UseGzip,
		client: &http.Client{
			Timeout: options.Timeout,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}, nil
}

func (c *Client) SendPrices(ctx context.Context, payload IngestPricesRequest) (SendResult, error) {
	var response IngestPricesResponse
	statusCode, duration, err := c.sendJSON(ctx, "/api/v1/ingest/prices", payload.RequestID, payload, &response)
	if err != nil {
		return SendResult{}, err
	}
	return SendResult{
		StatusCode: statusCode,
		Duration:   duration,
		Response:   response,
	}, nil
}

func (c *Client) SendHistory(ctx context.Context, payload IngestHistoryRequest) (HistorySendResult, error) {
	var response IngestHistoryResponse
	statusCode, duration, err := c.sendJSON(ctx, "/api/v1/ingest/history", payload.RequestID, payload, &response)
	if err != nil {
		return HistorySendResult{}, err
	}
	return HistorySendResult{
		StatusCode: statusCode,
		Duration:   duration,
		Response:   response,
	}, nil
}

func (c *Client) sendJSON(ctx context.Context, path string, requestID string, payload any, responsePayload any) (int, time.Duration, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return 0, 0, fmt.Errorf("marshal upstream payload: %w", err)
	}

	body, err := c.encodeBody(encoded)
	if err != nil {
		return 0, 0, &SendError{
			Category: observability.ErrorCategoryUpstreamPayload,
			Message:  err.Error(),
			Cause:    err,
		}
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return 0, 0, &SendError{
			Category: observability.ErrorCategoryInternal,
			Message:  fmt.Sprintf("build upstream request: %v", err),
			Cause:    err,
		}
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "albion-market-data-forwarder/1.0")
	request.Header.Set("Cache-Control", "no-store")
	if requestID = observability.CanonicalRequestID(requestID); requestID == "" {
		requestID = observability.RequestIDFromContext(ctx)
	}
	if requestID != "" {
		request.Header.Set(observability.HeaderRequestID, requestID)
	}
	if c.token != "" {
		request.Header.Set("Author" + "ization", strings.Join([]string{"Bearer", c.token}, " "))
	}
	if c.useGzip {
		request.Header.Set("Content-Encoding", "gzip")
	}

	startedAt := time.Now()
	response, err := c.client.Do(request)
	if err != nil {
		duration := time.Since(startedAt)
		return 0, duration, &SendError{
			Duration: duration,
			Category: observability.ErrorCategoryForError(err),
			Message:  fmt.Sprintf("send upstream request: %v", err),
			Cause:    err,
		}
	}
	defer response.Body.Close()

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		content, _ := io.ReadAll(io.LimitReader(response.Body, maxErrorBodyBytes))
		duration := time.Since(startedAt)
		message := strings.TrimSpace(string(content))
		if message == "" {
			message = http.StatusText(response.StatusCode)
		}
		return response.StatusCode, duration, &SendError{
			StatusCode: response.StatusCode,
			Duration:   duration,
			Category:   observability.ErrorCategoryForHTTPStatus(response.StatusCode),
			Message:    fmt.Sprintf("upstream returned %d: %s", response.StatusCode, message),
		}
	}

	content, err := io.ReadAll(io.LimitReader(response.Body, maxSuccessBodyBytes))
	duration := time.Since(startedAt)
	if err != nil {
		return response.StatusCode, duration, &SendError{
			StatusCode: response.StatusCode,
			Duration:   duration,
			Category:   observability.ErrorCategoryUpstreamResponse,
			Message:    fmt.Sprintf("read upstream response: %v", err),
			Cause:      err,
		}
	}
	if len(bytes.TrimSpace(content)) == 0 || responsePayload == nil {
		return response.StatusCode, duration, nil
	}
	if err := json.Unmarshal(content, responsePayload); err != nil {
		return response.StatusCode, duration, &SendError{
			StatusCode: response.StatusCode,
			Duration:   duration,
			Category:   observability.ErrorCategoryUpstreamResponse,
			Message:    "decode upstream response: invalid json",
			Cause:      err,
		}
	}
	return response.StatusCode, duration, nil
}

func (c *Client) encodeBody(raw []byte) ([]byte, error) {
	if !c.useGzip {
		return raw, nil
	}

	var buffer bytes.Buffer
	writer := gzip.NewWriter(&buffer)
	if _, err := writer.Write(raw); err != nil {
		return nil, fmt.Errorf("gzip upstream payload: %w", err)
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("finish gzip upstream payload: %w", err)
	}
	return buffer.Bytes(), nil
}

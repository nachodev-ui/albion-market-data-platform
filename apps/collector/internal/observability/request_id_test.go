package observability

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWithRequestIDPreservesValidHeader(t *testing.T) {
	var observed string
	handler := WithRequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observed = RequestIDFromContext(r.Context())
		w.WriteHeader(http.StatusNoContent)
	}))

	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	request.Header.Set(HeaderRequestID, "client-req-123456")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if observed != "client-req-123456" {
		t.Fatalf("observed request id = %q", observed)
	}
	if response.Header().Get(HeaderRequestID) != "client-req-123456" {
		t.Fatalf("response request id = %q", response.Header().Get(HeaderRequestID))
	}
}

func TestWithRequestIDGeneratesWhenHeaderIsUnsafe(t *testing.T) {
	var observed string
	handler := WithRequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observed = RequestIDFromContext(r.Context())
		w.WriteHeader(http.StatusNoContent)
	}))

	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	request.Header.Set(HeaderRequestID, "bad header with spaces")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if observed == "" || observed == "bad header with spaces" {
		t.Fatalf("unsafe request id was not replaced: %q", observed)
	}
	if response.Header().Get(HeaderRequestID) != observed {
		t.Fatalf("response request id = %q, want %q", response.Header().Get(HeaderRequestID), observed)
	}
}

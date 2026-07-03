package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"albion-market-data/collector/internal/storage/queryjsonl"
)

func TestHealthAndReadinessEndpoints(t *testing.T) {
	handler, err := NewHandler(&emptyRepository{}, testAPICatalog(t), StatusConfig{ServiceName: "receiver-test"})
	if err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		path       string
		wantStatus int
		wantBody   string
	}{
		{path: RouteHealth, wantStatus: http.StatusOK, wantBody: `"status":"ok"`},
		{path: RouteReady, wantStatus: http.StatusOK, wantBody: `"status":"ready"`},
	} {
		request := httptest.NewRequest(http.MethodGet, test.path, nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != test.wantStatus {
			t.Fatalf("%s status=%d body=%s", test.path, response.Code, response.Body.String())
		}
		if !strings.Contains(response.Body.String(), test.wantBody) || !strings.Contains(response.Body.String(), `"service":"receiver-test"`) {
			t.Fatalf("%s body=%s", test.path, response.Body.String())
		}
		if response.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Fatalf("%s missing nosniff header", test.path)
		}
		if response.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Fatalf("%s unexpectedly exposed wildcard CORS", test.path)
		}
	}
}

type unreadyRepository struct{ emptyRepository }

func (*unreadyRepository) Stats() queryjsonl.RepositoryStats {
	return queryjsonl.RepositoryStats{}
}

func TestReadinessReportsUnavailableRepository(t *testing.T) {
	handler, err := NewHandler(&unreadyRepository{}, testAPICatalog(t))
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, RouteReady, nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"repository":"unavailable"`) {
		t.Fatalf("body=%s", response.Body.String())
	}
}

func TestLocalAPICORSUsesConfiguredAllowlist(t *testing.T) {
	handler, err := NewHandler(&emptyRepository{}, testAPICatalog(t), StatusConfig{
		AllowedOrigins: []string{"http://127.0.0.1:4173"},
	})
	if err != nil {
		t.Fatal(err)
	}

	allowed := httptest.NewRequest(http.MethodOptions, RouteMarkets, nil)
	allowed.Header.Set("Origin", "http://127.0.0.1:4173")
	allowedResponse := httptest.NewRecorder()
	handler.ServeHTTP(allowedResponse, allowed)
	if allowedResponse.Code != http.StatusNoContent {
		t.Fatalf("allowed status=%d body=%s", allowedResponse.Code, allowedResponse.Body.String())
	}
	if value := allowedResponse.Header().Get("Access-Control-Allow-Origin"); value != "http://127.0.0.1:4173" {
		t.Fatalf("allowed origin=%q", value)
	}
	if value := allowedResponse.Header().Get("Vary"); value != "Origin" {
		t.Fatalf("vary=%q", value)
	}

	denied := httptest.NewRequest(http.MethodGet, RouteMarkets, nil)
	denied.Header.Set("Origin", "https://untrusted.example")
	deniedResponse := httptest.NewRecorder()
	handler.ServeHTTP(deniedResponse, denied)
	if deniedResponse.Code != http.StatusForbidden {
		t.Fatalf("denied status=%d body=%s", deniedResponse.Code, deniedResponse.Body.String())
	}
	if deniedResponse.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("denied origin must not be reflected")
	}
}

func TestLocalAPIRejectsUnsupportedMethodsAndOversizedQueries(t *testing.T) {
	handler, err := NewHandler(&emptyRepository{}, testAPICatalog(t), StatusConfig{MaxQueryBytes: 32})
	if err != nil {
		t.Fatal(err)
	}

	post := httptest.NewRequest(http.MethodPost, RouteMarkets, nil)
	postResponse := httptest.NewRecorder()
	handler.ServeHTTP(postResponse, post)
	if postResponse.Code != http.StatusMethodNotAllowed || postResponse.Header().Get("Allow") != "GET, OPTIONS" {
		t.Fatalf("post status=%d allow=%q", postResponse.Code, postResponse.Header().Get("Allow"))
	}

	oversized := httptest.NewRequest(http.MethodGet, RouteMarkets+"?value="+strings.Repeat("a", 40), nil)
	oversizedResponse := httptest.NewRecorder()
	handler.ServeHTTP(oversizedResponse, oversized)
	if oversizedResponse.Code != http.StatusRequestURITooLong {
		t.Fatalf("oversized status=%d body=%s", oversizedResponse.Code, oversizedResponse.Body.String())
	}
}

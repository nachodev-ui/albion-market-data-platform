package httpapi

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

func setSecurityHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
}

func (h *Handler) applyCORS(w http.ResponseWriter, r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	w.Header().Add("Vary", "Origin")
	if _, allowed := h.allowedOrigins[origin]; !allowed {
		writeError(w, http.StatusForbidden, "origin is not allowed")
		return false
	}
	w.Header().Set("Access-Control-Allow-Origin", origin)
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	w.Header().Set("Access-Control-Max-Age", "600")
	return true
}

func isDocumentedRoute(path string) bool {
	for _, route := range DocumentedGETRoutes {
		if path == route {
			return true
		}
	}
	return false
}

func (h *Handler) validateQuery(target *url.URL) (int, error) {
	if len(target.RawQuery) > h.maxQueryBytes {
		return http.StatusRequestURITooLong, fmt.Errorf("query string exceeds %d bytes", h.maxQueryBytes)
	}
	values, err := url.ParseQuery(target.RawQuery)
	if err != nil {
		return http.StatusBadRequest, fmt.Errorf("query string is invalid")
	}
	count := 0
	for key, entries := range values {
		if len(key) > maxQueryKeyBytes {
			return http.StatusBadRequest, fmt.Errorf("query parameter name exceeds %d bytes", maxQueryKeyBytes)
		}
		count += len(entries)
		if count > h.maxQueryValues {
			return http.StatusBadRequest, fmt.Errorf("query string contains more than %d values", h.maxQueryValues)
		}
		for _, value := range entries {
			if len(value) > maxQueryValueBytes {
				return http.StatusBadRequest, fmt.Errorf("query parameter value exceeds %d bytes", maxQueryValueBytes)
			}
		}
	}
	return 0, nil
}

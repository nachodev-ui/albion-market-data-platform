package main

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

func validateListenAddress(address string, allowRemote bool) error {
	host, port, err := net.SplitHostPort(strings.TrimSpace(address))
	if err != nil {
		return fmt.Errorf("COLLECTOR_LISTEN must be a host:port address: %w", err)
	}
	if strings.TrimSpace(port) == "" {
		return fmt.Errorf("COLLECTOR_LISTEN requires a port")
	}
	host = strings.TrimSpace(host)
	if allowRemote {
		return nil
	}
	if strings.EqualFold(host, "localhost") {
		return nil
	}
	ip := net.ParseIP(host)
	if ip != nil && ip.IsLoopback() {
		return nil
	}
	return fmt.Errorf("COLLECTOR_LISTEN %q exposes the receiver beyond loopback; set COLLECTOR_ALLOW_REMOTE=true only behind a trusted firewall", address)
}

func parseAllowedOrigins(value string) ([]string, error) {
	seen := make(map[string]struct{})
	origins := make([]string, 0)
	for _, candidate := range strings.Split(value, ",") {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if candidate == "*" {
			return nil, fmt.Errorf("wildcard origin is not allowed")
		}
		parsed, err := url.Parse(candidate)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
			return nil, fmt.Errorf("invalid browser origin %q", candidate)
		}
		normalized := parsed.Scheme + "://" + parsed.Host
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}
		origins = append(origins, normalized)
	}
	return origins, nil
}

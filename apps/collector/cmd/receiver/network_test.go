package main

import "testing"

func TestValidateListenAddressRequiresLoopbackByDefault(t *testing.T) {
	for _, address := range []string{"127.0.0.1:8787", "[::1]:8787", "localhost:8787"} {
		if err := validateListenAddress(address, false); err != nil {
			t.Fatalf("%s: %v", address, err)
		}
	}
	for _, address := range []string{"0.0.0.0:8787", ":8787", "192.168.1.10:8787"} {
		if err := validateListenAddress(address, false); err == nil {
			t.Fatalf("%s unexpectedly accepted", address)
		}
	}
	if err := validateListenAddress("0.0.0.0:8787", true); err != nil {
		t.Fatalf("explicit remote listen: %v", err)
	}
}

func TestParseAllowedOriginsRejectsWildcardAndPaths(t *testing.T) {
	origins, err := parseAllowedOrigins("http://127.0.0.1:5173, http://localhost:5173,http://localhost:5173")
	if err != nil {
		t.Fatal(err)
	}
	if len(origins) != 2 {
		t.Fatalf("origins=%v", origins)
	}
	for _, value := range []string{"*", "http://localhost:5173/path", "file:///tmp/index.html"} {
		if _, err := parseAllowedOrigins(value); err == nil {
			t.Fatalf("origin %q unexpectedly accepted", value)
		}
	}
}

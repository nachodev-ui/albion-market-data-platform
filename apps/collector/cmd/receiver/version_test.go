package main

import (
	"strings"
	"testing"

	"albion-market-data/collector/internal/observability"
)

func TestIsVersionRequest(t *testing.T) {
	t.Parallel()

	cases := []struct {
		args []string
		want bool
	}{
		{args: []string{"--version"}, want: true},
		{args: []string{"-version"}, want: true},
		{args: []string{"version"}, want: true},
		{args: []string{"--listen", "127.0.0.1:8787"}, want: false},
	}

	for _, testCase := range cases {
		if got := isVersionRequest(testCase.args); got != testCase.want {
			t.Fatalf("isVersionRequest(%v) = %v, want %v", testCase.args, got, testCase.want)
		}
	}
}

func TestFormatVersion(t *testing.T) {
	t.Parallel()

	output := formatVersion(observability.BuildInfo{
		Version:   "1.2.3",
		Commit:    "abcdef",
		BuiltAt:   "2026-07-06T08:00:00Z",
		GoVersion: "go1.23.0",
		Modified:  false,
	})

	for _, expected := range []string{
		"albion-market-receiver",
		"version=1.2.3",
		"commit=abcdef",
		"built_at=2026-07-06T08:00:00Z",
		"go=go1.23.0",
		"modified=false",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("version output %q does not contain %q", output, expected)
		}
	}
}

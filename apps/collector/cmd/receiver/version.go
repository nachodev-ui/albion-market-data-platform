package main

import (
	"fmt"
	"strings"

	"albion-market-data/collector/internal/observability"
)

func isVersionRequest(args []string) bool {
	for _, arg := range args {
		switch strings.TrimSpace(arg) {
		case "--version", "-version", "version":
			return true
		}
	}
	return false
}

func formatVersion(info observability.BuildInfo) string {
	builtAt := strings.TrimSpace(info.BuiltAt)
	if builtAt == "" {
		builtAt = "unknown"
	}
	return fmt.Sprintf("albion-market-receiver version=%s commit=%s built_at=%s go=%s modified=%t",
		info.Version,
		info.Commit,
		builtAt,
		info.GoVersion,
		info.Modified,
	)
}

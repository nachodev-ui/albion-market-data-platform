package observability

import (
	"runtime"
	"runtime/debug"
	"strings"
)

var (
	BuildVersion = "dev"
	BuildCommit  = "unknown"
	BuildTime    = ""
)

type BuildInfo struct {
	Version   string
	Commit    string
	BuiltAt   string
	GoVersion string
	Modified  bool
}

func CurrentBuildInfo() BuildInfo {
	result := BuildInfo{
		Version:   strings.TrimSpace(BuildVersion),
		Commit:    strings.TrimSpace(BuildCommit),
		BuiltAt:   strings.TrimSpace(BuildTime),
		GoVersion: runtime.Version(),
	}
	if result.Version == "" {
		result.Version = "dev"
	}
	if result.Commit == "" {
		result.Commit = "unknown"
	}

	if info, ok := debug.ReadBuildInfo(); ok {
		if result.Version == "dev" && strings.TrimSpace(info.Main.Version) != "" && info.Main.Version != "(devel)" {
			result.Version = info.Main.Version
		}
		for _, setting := range info.Settings {
			switch setting.Key {
			case "vcs.revision":
				if result.Commit == "unknown" && strings.TrimSpace(setting.Value) != "" {
					result.Commit = setting.Value
				}
			case "vcs.time":
				if result.BuiltAt == "" {
					result.BuiltAt = setting.Value
				}
			case "vcs.modified":
				result.Modified = setting.Value == "true"
			}
		}
	}
	return result
}

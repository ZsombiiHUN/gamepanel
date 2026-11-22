// Package version reports which build of GamePanel is running.
package version

import (
	"fmt"
	"runtime"
	"runtime/debug"
)

// Set at release time with -ldflags "-X github.com/ZsombiiHUN/gamepanel/internal/version.Version=v0.1.0 ...".
var (
	Version = "dev"
	Commit  = ""
	Date    = ""
)

// String is the one-line version both binaries print, e.g.
// "panel v0.1.0 (commit 1a2b3c4, built 2026-11-22, go1.27.4 linux/amd64)".
func String(binary string) string {
	commit, date, dirty := Commit, Date, false
	if commit == "" {
		// A plain `go build` from a git checkout still records the commit.
		if info, ok := debug.ReadBuildInfo(); ok {
			for _, s := range info.Settings {
				switch s.Key {
				case "vcs.revision":
					commit = s.Value
				case "vcs.time":
					if date == "" {
						date = s.Value
					}
				case "vcs.modified":
					dirty = s.Value == "true"
				}
			}
		}
	}
	if len(commit) > 12 {
		commit = commit[:12]
	}
	if len(date) > 10 {
		date = date[:10]
	}
	if commit == "" {
		commit = "unknown"
	} else if dirty {
		commit += "-dirty"
	}
	if date == "" {
		date = "unknown"
	}
	return fmt.Sprintf("%s %s (commit %s, built %s, %s %s/%s)",
		binary, Version, commit, date, runtime.Version(), runtime.GOOS, runtime.GOARCH)
}

// IsFlag reports whether a command-line argument asks for the version.
func IsFlag(arg string) bool {
	return arg == "version" || arg == "--version" || arg == "-version" || arg == "-v"
}

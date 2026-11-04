// Package version holds build metadata for RSCON.
package version

import (
	"fmt"
	"runtime"
)

// These variables are overridden at build time with -ldflags.
var (
	Version   = "0.1.0-dev"
	Commit    = "unknown"
	BuildDate = "unknown"
)

// Info is the machine-readable version payload.
type Info struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildDate string `json:"build_date"`
	GoVersion string `json:"go_version"`
	Platform  string `json:"platform"`
}

// Get returns the current build information.
func Get() Info {
	return Info{
		Version:   Version,
		Commit:    Commit,
		BuildDate: BuildDate,
		GoVersion: runtime.Version(),
		Platform:  runtime.GOOS + "/" + runtime.GOARCH,
	}
}

// String returns a human-readable one-line version.
func String() string {
	return fmt.Sprintf("rscon %s (commit %s, built %s, %s)", Version, Commit, BuildDate, runtime.Version())
}

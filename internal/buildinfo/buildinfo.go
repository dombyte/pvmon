// Package buildinfo carries the version information set by the linker.
package buildinfo

import (
	"fmt"
	"runtime"
)

// Info describes one build of the binary.
type Info struct {
	Version   string
	Commit    string
	BuildDate string
	GoVersion string
}

// New returns the build info; an empty version becomes "dev".
func New(version, commit, date string) Info {
	if version == "" {
		version = "dev"
	}
	return Info{Version: version, Commit: commit, BuildDate: date, GoVersion: runtime.Version()}
}

// String renders the info on one line.
func (i Info) String() string {
	return fmt.Sprintf("pvmon %s (commit %s, built %s, %s)",
		i.Version, orUnknown(i.Commit), orUnknown(i.BuildDate), i.GoVersion)
}

func orUnknown(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}

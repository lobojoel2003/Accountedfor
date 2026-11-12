// Package version exposes build metadata that is stamped into the binary at
// link time:
//
//	go build -ldflags "-X github.com/lobojoel2003/accountedfor/internal/version.Version=v1.2.3 ..."
//
// Release builds set every field. Local builds fall back to the VCS
// information recorded by the Go toolchain.
package version

import (
	"fmt"
	"runtime"
	"runtime/debug"
	"strings"
)

// These are overridden with -ldflags "-X ..." by the Makefile, Dockerfile and
// release workflow.
var (
	// Version is the semantic version of the release, e.g. "v1.0.0".
	Version = "dev"
	// Commit is the git commit the binary was built from.
	Commit = ""
	// BuildNumber is the CI run number that produced the binary.
	BuildNumber = "0"
	// BuildDate is the UTC build time in RFC 3339 format.
	BuildDate = ""
)

func init() {
	if Commit != "" && BuildDate != "" {
		return
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return
	}
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			if Commit == "" {
				Commit = s.Value
			}
		case "vcs.time":
			if BuildDate == "" {
				BuildDate = s.Value
			}
		}
	}
}

// ShortCommit returns the first 7 characters of the commit hash.
func ShortCommit() string {
	if len(Commit) > 7 {
		return Commit[:7]
	}
	if Commit == "" {
		return "unknown"
	}
	return Commit
}

// Info is the machine readable build description served at /version.
type Info struct {
	Version     string `json:"version"`
	BuildNumber string `json:"build"`
	Commit      string `json:"commit"`
	BuildDate   string `json:"build_date"`
	GoVersion   string `json:"go_version"`
	Platform    string `json:"platform"`
}

// Get returns the build information of the running binary.
func Get() Info {
	return Info{
		Version:     Version,
		BuildNumber: BuildNumber,
		Commit:      Commit,
		BuildDate:   BuildDate,
		GoVersion:   runtime.Version(),
		Platform:    runtime.GOOS + "/" + runtime.GOARCH,
	}
}

// String returns a human readable one-line description such as
// "v1.0.0 (build 42, 1a2b3c4)".
func String() string {
	var b strings.Builder
	b.WriteString(Version)
	fmt.Fprintf(&b, " (build %s, %s", BuildNumber, ShortCommit())
	if BuildDate != "" {
		date := BuildDate
		if len(date) >= 10 {
			date = date[:10]
		}
		fmt.Fprintf(&b, ", %s", date)
	}
	b.WriteString(")")
	return b.String()
}

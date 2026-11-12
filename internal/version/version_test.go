package version

import (
	"strings"
	"testing"
)

func TestString(t *testing.T) {
	old := [4]string{Version, Commit, BuildNumber, BuildDate}
	defer func() { Version, Commit, BuildNumber, BuildDate = old[0], old[1], old[2], old[3] }()
	Version, Commit, BuildNumber, BuildDate = "v1.2.3", "0123456789abcdef", "42", "2026-11-12T10:00:00Z"
	if got := String(); got != "v1.2.3 (build 42, 0123456, 2026-11-12)" {
		t.Errorf("String() = %q", got)
	}
	info := Get()
	if info.Version != "v1.2.3" || info.BuildNumber != "42" || !strings.HasPrefix(info.GoVersion, "go") {
		t.Errorf("Get() = %+v", info)
	}
	Commit = ""
	if ShortCommit() != "unknown" {
		t.Error("empty commit should be unknown")
	}
}

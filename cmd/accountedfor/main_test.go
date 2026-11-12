package main

import (
	"testing"
)

func TestRunWithoutDatabase(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	if err := run([]string{"version"}); err != nil {
		t.Errorf("version: %v", err)
	}
	if err := run([]string{"help"}); err != nil {
		t.Errorf("help: %v", err)
	}
	if err := run([]string{"serve"}); err == nil {
		t.Error("serve without DATABASE_URL should fail")
	}
	if err := run([]string{"healthcheck", "http://127.0.0.1:1/healthz"}); err == nil {
		t.Error("healthcheck against a closed port should fail")
	}
}

func TestSplitName(t *testing.T) {
	name, rest := splitName([]string{"alice", "--admin"})
	if name != "alice" || len(rest) != 1 {
		t.Errorf("splitName = %q %v", name, rest)
	}
	name, rest = splitName([]string{"--admin"})
	if name != "" || len(rest) != 1 {
		t.Errorf("splitName = %q %v", name, rest)
	}
}

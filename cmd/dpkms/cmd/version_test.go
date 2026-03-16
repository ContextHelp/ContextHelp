package cmd

import (
	"strings"
	"testing"
)

func TestVersionFlag(t *testing.T) {
	SetVersionInfo("1.2.3", "2026-03-15_10:00:00", "abc1234")
	defer SetVersionInfo("", "", "")

	out, err := executeCommand("-v")
	if err != nil {
		t.Fatalf("-v should succeed: %v", err)
	}
	if !strings.Contains(out, "dpkms version 1.2.3") {
		t.Errorf("output should contain 'dpkms version 1.2.3', got: %s", out)
	}
	if !strings.Contains(out, "(2026-03-15)") {
		t.Errorf("output should contain '(2026-03-15)', got: %s", out)
	}
}

func TestVersionFlagLong(t *testing.T) {
	SetVersionInfo("0.1.0-dirty", "2026-03-15_10:00:00", "abc1234")
	defer SetVersionInfo("", "", "")

	out, err := executeCommand("--version")
	if err != nil {
		t.Fatalf("--version should succeed: %v", err)
	}
	if !strings.Contains(out, "dpkms version 0.1.0-dirty (2026-03-15)") {
		t.Errorf("output format mismatch, got: %s", out)
	}
}

package cmd

import (
	"strings"
	"testing"
)

func TestVersion(t *testing.T) {
	out, err := executeCommand("version")
	if err != nil {
		t.Fatalf("version should succeed: %v", err)
	}
	if !strings.Contains(out, "dPKMS") {
		t.Error("version output should contain 'dPKMS'")
	}
	if !strings.Contains(out, "dpkms version:") {
		t.Error("version output should contain 'dpkms version:'")
	}
	if !strings.Contains(out, "Component:") {
		t.Error("version output should contain 'Component:'")
	}
	if !strings.Contains(out, "substrate") {
		t.Error("version output should identify as substrate component")
	}
	if !strings.Contains(out, "Registry Protocol:") {
		t.Error("version output should contain 'Registry Protocol:'")
	}
}

func TestVersionInfo(t *testing.T) {
	SetVersionInfo("2.0.0", "2025-02-01", "def456")
	defer SetVersionInfo("", "", "")

	out, err := executeCommand("version")
	if err != nil {
		t.Fatalf("version should succeed: %v", err)
	}
	if !strings.Contains(out, "2.0.0") {
		t.Error("version output should contain set version")
	}
	if !strings.Contains(out, "def456") {
		t.Error("version output should contain set git commit")
	}
}

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
	if !strings.Contains(out, "ContextHelp CLI") {
		t.Error("version output should contain 'ContextHelp CLI'")
	}
	if !strings.Contains(out, "ctxt version:") {
		t.Error("version output should contain 'ctxt version:'")
	}
	if !strings.Contains(out, "Component:") {
		t.Error("version output should contain 'Component:'")
	}
	if !strings.Contains(out, "Registry Protocol:") {
		t.Error("version output should contain 'Registry Protocol:'")
	}
}

func TestVersionInfo(t *testing.T) {
	SetVersionInfo("1.0.0", "2025-01-01", "abc123")
	defer SetVersionInfo("", "", "")

	out, err := executeCommand("version")
	if err != nil {
		t.Fatalf("version should succeed: %v", err)
	}
	if !strings.Contains(out, "1.0.0") {
		t.Error("version output should contain set version")
	}
	if !strings.Contains(out, "abc123") {
		t.Error("version output should contain set git commit")
	}
}

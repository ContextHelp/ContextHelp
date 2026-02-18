package cmd

import (
	"strings"
	"testing"
)

func TestFind(t *testing.T) {
	out, err := executeCommand("find", "authentication best practices")
	if err != nil {
		t.Fatalf("find should succeed: %v", err)
	}
	if !strings.Contains(out, "Searching for: authentication best practices") {
		t.Error("output should show query")
	}
	if !strings.Contains(out, "Search Results:") {
		t.Error("output should contain search results header")
	}
}

func TestFindWithLimit(t *testing.T) {
	out, err := executeCommand("find", "onboarding", "--limit", "5")
	if err != nil {
		t.Fatalf("find with --limit should succeed: %v", err)
	}
	if !strings.Contains(out, "Limit: 5") {
		t.Error("output should reflect limit")
	}
}

func TestFindNoQueryError(t *testing.T) {
	_, err := executeCommand("find")
	if err == nil {
		t.Error("find without query should fail")
	}
}

func TestFindHelp(t *testing.T) {
	out, err := executeCommand("find", "--help")
	if err != nil {
		t.Fatalf("find --help should succeed: %v", err)
	}
	if !strings.Contains(out, "--limit") {
		t.Error("find help should list --limit flag")
	}
	if !strings.Contains(out, "search") {
		t.Error("find help should describe search functionality")
	}
}

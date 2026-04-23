package cmd

import (
	"strings"
	"testing"
)

func TestLintHelp(t *testing.T) {
	out, err := executeCommand("lint", "--help")
	if err != nil {
		t.Fatalf("lint --help should succeed: %v", err)
	}
	for _, want := range []string{"lint", "--check", "--limit", "--profile"} {
		if !strings.Contains(out, want) {
			t.Errorf("help output missing %q", want)
		}
	}
}

func TestLintText(t *testing.T) {
	db := setupTestDB(t)
	out, err := db.exec("lint")
	if err != nil {
		t.Fatalf("lint should succeed: %v", err)
	}
	if !strings.Contains(out, "Lint checks") {
		t.Errorf("expected lint header in output, got: %s", out)
	}
}

func TestLintJSON(t *testing.T) {
	db := setupTestDB(t)
	out, err := db.exec("--output", "json", "lint")
	if err != nil {
		t.Fatalf("lint --output json should succeed: %v", err)
	}
	for _, want := range []string{`"checks"`, `"issues"`, `"duration"`} {
		if !strings.Contains(out, want) {
			t.Errorf("json output missing key %q", want)
		}
	}
}

func TestLintCheckFlag(t *testing.T) {
	db := setupTestDB(t)
	out, err := db.exec("--output", "json", "lint", "--check", "orphans")
	if err != nil {
		t.Fatalf("lint --check orphans should succeed: %v", err)
	}
	if !strings.Contains(out, `"orphans"`) {
		t.Errorf("expected orphans check in output")
	}
	// should not contain other check names in the checks array
	if strings.Contains(out, `"stale"`) {
		t.Errorf("should not run stale check when only orphans requested")
	}
}

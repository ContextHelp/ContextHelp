package cmd

import (
	"strings"
	"testing"
)

func TestDoctorHelp(t *testing.T) {
	out, err := executeCommand("doctor", "--help")
	if err != nil {
		t.Fatalf("doctor --help should succeed: %v", err)
	}
	for _, want := range []string{"doctor", "--check", "--limit", "--profile"} {
		if !strings.Contains(out, want) {
			t.Errorf("help output missing %q", want)
		}
	}
}

func TestDoctorText(t *testing.T) {
	db := setupTestDB(t)
	out, err := db.exec("doctor")
	if err != nil {
		t.Fatalf("doctor should succeed: %v", err)
	}
	if !strings.Contains(out, "Lint checks") {
		t.Errorf("expected doctor header in output, got: %s", out)
	}
}

func TestDoctorJSON(t *testing.T) {
	db := setupTestDB(t)
	out, err := db.exec("--format", "json", "doctor")
	if err != nil {
		t.Fatalf("doctor --output json should succeed: %v", err)
	}
	for _, want := range []string{`"checks"`, `"issues"`, `"duration"`} {
		if !strings.Contains(out, want) {
			t.Errorf("json output missing key %q", want)
		}
	}
}

func TestDoctorCheckFlag(t *testing.T) {
	db := setupTestDB(t)
	out, err := db.exec("--format", "json", "doctor", "--check", "orphans")
	if err != nil {
		t.Fatalf("doctor --check orphans should succeed: %v", err)
	}
	if !strings.Contains(out, `"orphans"`) {
		t.Errorf("expected orphans check in output")
	}
	// should not contain other check names in the checks array
	if strings.Contains(out, `"stale"`) {
		t.Errorf("should not run stale check when only orphans requested")
	}
}

package cmd

import (
	"strings"
	"testing"
)

func TestStatsHelp(t *testing.T) {
	out, err := executeCommand("stats", "--help")
	if err != nil {
		t.Fatalf("stats --help should succeed: %v", err)
	}
	for _, want := range []string{"stats", "--watch", "--format"} {
		if !strings.Contains(out, want) {
			t.Errorf("help output missing %q", want)
		}
	}
}

func TestStatsText(t *testing.T) {
	db := setupTestDB(t)
	out, err := db.exec("stats")
	if err != nil {
		t.Fatalf("stats should succeed: %v", err)
	}
	for _, want := range []string{
		"Knowledge Objects",
		"Jobs",
		"Entities",
		"Feeds",
		"Profiles",
		"Reminders",
		"Resurfacing",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("stats output missing field %q", want)
		}
	}
}

func TestStatsJSON(t *testing.T) {
	db := setupTestDB(t)
	out, err := db.exec("--format", "json", "stats")
	if err != nil {
		t.Fatalf("stats --format json should succeed: %v", err)
	}
	for _, want := range []string{
		`"knowledge_objects"`,
		`"jobs"`,
		`"entities"`,
		`"feeds"`,
		`"profiles"`,
		`"reminders_pending"`,
		`"resurfacing_candidates"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("json output missing key %q", want)
		}
	}
}

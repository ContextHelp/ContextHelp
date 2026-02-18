package cmd

import (
	"strings"
	"testing"
)

func TestList(t *testing.T) {
	out, err := executeCommand("list")
	if err != nil {
		t.Fatalf("list should succeed: %v", err)
	}
	if !strings.Contains(out, "Knowledge Objects:") {
		t.Error("output should contain 'Knowledge Objects:'")
	}
	if !strings.Contains(out, "obj_001") {
		t.Error("output should contain sample object IDs")
	}
}

func TestListWithFilters(t *testing.T) {
	out, err := executeCommand("list", "--type", "url", "--tag", "ux", "--mention", "@ui.best-practice")
	if err != nil {
		t.Fatalf("list with filters should succeed: %v", err)
	}
	if !strings.Contains(out, "type=url") {
		t.Error("output should reflect type filter")
	}
	if !strings.Contains(out, "tag=ux") {
		t.Error("output should reflect tag filter")
	}
}

func TestListWithDisplayFlags(t *testing.T) {
	out, err := executeCommand("list", "--limit", "10", "--sort", "match", "--dir", "asc")
	if err != nil {
		t.Fatalf("list with display flags should succeed: %v", err)
	}
	if !strings.Contains(out, "match") {
		t.Error("output should reflect sort setting")
	}
	if !strings.Contains(out, "asc") {
		t.Error("output should reflect dir setting")
	}
}

func TestListHelp(t *testing.T) {
	out, err := executeCommand("list", "--help")
	if err != nil {
		t.Fatalf("list --help should succeed: %v", err)
	}
	for _, flag := range []string{"--type", "--tag", "--hint", "--mention", "--pipeline", "--subtype", "--before", "--after", "--q", "--limit", "--start", "--sort", "--dir", "--no-track"} {
		if !strings.Contains(out, flag) {
			t.Errorf("list help should list flag %s", flag)
		}
	}
}

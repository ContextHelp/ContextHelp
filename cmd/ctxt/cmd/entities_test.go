package cmd

import (
	"strings"
	"testing"
)

func TestEntitiesList(t *testing.T) {
	out, err := executeCommand("entity", "list")
	if err != nil {
		t.Fatalf("entity list should succeed: %v", err)
	}
	if !strings.Contains(out, "Entities") {
		t.Error("output should contain entities header")
	}
}

func TestEntitiesShow(t *testing.T) {
	_, err := executeCommand("entity", "show", "ui.best-practice")
	if err == nil {
		// If entity doesn't exist in test DB, error is expected.
		// If it succeeds, verify output format.
		return
	}
	if !strings.Contains(err.Error(), "get entity") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestEntitiesShowNoSlugError(t *testing.T) {
	_, err := executeCommand("entity", "show")
	if err == nil {
		t.Error("entity show without slug should fail")
	}
}

func TestEntitiesSearch(t *testing.T) {
	out, err := executeCommand("entity", "search", "checkout")
	if err != nil {
		t.Fatalf("entity search should succeed: %v", err)
	}
	if !strings.Contains(out, "Entity search:") {
		t.Error("output should show search header")
	}
	if !strings.Contains(out, "checkout") {
		t.Error("output should echo the search query")
	}
}

func TestEntitiesSearchNoQueryError(t *testing.T) {
	_, err := executeCommand("entity", "search")
	if err == nil {
		t.Error("entity search without query should fail")
	}
}

func TestEntitiesBacklinks(t *testing.T) {
	_, err := executeCommand("entity", "backlink", "ui.best-practice")
	if err == nil {
		return
	}
	if !strings.Contains(err.Error(), "backlinks") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestEntitiesBacklinksNoSlugError(t *testing.T) {
	_, err := executeCommand("entity", "backlink")
	if err == nil {
		t.Error("entity backlink without slug should fail")
	}
}

func TestEntitiesHelp(t *testing.T) {
	out, err := executeCommand("entity", "--help")
	if err != nil {
		t.Fatalf("entity --help should succeed: %v", err)
	}
	for _, subcmd := range []string{"list", "show", "search", "backlink"} {
		if !strings.Contains(out, subcmd) {
			t.Errorf("entity help should list subcommand %q", subcmd)
		}
	}
}

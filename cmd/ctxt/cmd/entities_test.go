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
	if !strings.Contains(out, "ui.best-practice") {
		t.Error("output should list sample entities")
	}
}

func TestEntitiesShow(t *testing.T) {
	out, err := executeCommand("entity", "show", "ui.best-practice")
	if err != nil {
		t.Fatalf("entity show should succeed: %v", err)
	}
	if !strings.Contains(out, "Entity: ui.best-practice") {
		t.Error("output should show entity slug")
	}
	if !strings.Contains(out, "Title:") {
		t.Error("output should contain Title field")
	}
	if !strings.Contains(out, "Namespace:") {
		t.Error("output should contain Namespace field")
	}
	if !strings.Contains(out, "Aliases:") {
		t.Error("output should contain Aliases section")
	}
	if !strings.Contains(out, "Backlinks:") {
		t.Error("output should contain Backlinks count")
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
	if !strings.Contains(out, "Searching entities for: checkout") {
		t.Error("output should show search query")
	}
	if !strings.Contains(out, "checkout.flow") {
		t.Error("output should list matching entities")
	}
}

func TestEntitiesSearchNoQueryError(t *testing.T) {
	_, err := executeCommand("entity", "search")
	if err == nil {
		t.Error("entity search without query should fail")
	}
}

func TestEntitiesBacklinks(t *testing.T) {
	out, err := executeCommand("entity", "backlink", "ui.best-practice")
	if err != nil {
		t.Fatalf("entity backlink should succeed: %v", err)
	}
	if !strings.Contains(out, "Backlinks for entity: ui.best-practice") {
		t.Error("output should show entity slug")
	}
	if !strings.Contains(out, "Knowledge objects that mention this entity:") {
		t.Error("output should describe backlinks")
	}
	if !strings.Contains(out, "obj_001") {
		t.Error("output should list linked objects")
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

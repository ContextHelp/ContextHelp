package cmd

import (
	"strings"
	"testing"
)

func TestEntitiesList(t *testing.T) {
	out, err := executeCommand("entities", "list")
	if err != nil {
		t.Fatalf("entities list should succeed: %v", err)
	}
	if !strings.Contains(out, "Entities") {
		t.Error("output should contain entities header")
	}
	if !strings.Contains(out, "ui.best-practice") {
		t.Error("output should list sample entities")
	}
}

func TestEntitiesShow(t *testing.T) {
	out, err := executeCommand("entities", "show", "ui.best-practice")
	if err != nil {
		t.Fatalf("entities show should succeed: %v", err)
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
	_, err := executeCommand("entities", "show")
	if err == nil {
		t.Error("entities show without slug should fail")
	}
}

func TestEntitiesSearch(t *testing.T) {
	out, err := executeCommand("entities", "search", "checkout")
	if err != nil {
		t.Fatalf("entities search should succeed: %v", err)
	}
	if !strings.Contains(out, "Searching entities for: checkout") {
		t.Error("output should show search query")
	}
	if !strings.Contains(out, "checkout.flow") {
		t.Error("output should list matching entities")
	}
}

func TestEntitiesSearchNoQueryError(t *testing.T) {
	_, err := executeCommand("entities", "search")
	if err == nil {
		t.Error("entities search without query should fail")
	}
}

func TestEntitiesBacklinks(t *testing.T) {
	out, err := executeCommand("entities", "backlinks", "ui.best-practice")
	if err != nil {
		t.Fatalf("entities backlinks should succeed: %v", err)
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
	_, err := executeCommand("entities", "backlinks")
	if err == nil {
		t.Error("entities backlinks without slug should fail")
	}
}

func TestEntitiesHelp(t *testing.T) {
	out, err := executeCommand("entities", "--help")
	if err != nil {
		t.Fatalf("entities --help should succeed: %v", err)
	}
	for _, subcmd := range []string{"list", "show", "search", "backlinks"} {
		if !strings.Contains(out, subcmd) {
			t.Errorf("entities help should list subcommand %q", subcmd)
		}
	}
}

package cmd

import (
	"strings"
	"testing"
)

func TestDeleteByIDWithYes(t *testing.T) {
	out, err := executeCommand("delete", "--id", "obj_123", "-y")
	if err != nil {
		t.Fatalf("delete --id with -y should succeed: %v", err)
	}
	if !strings.Contains(out, "ID: obj_123") {
		t.Error("output should show filter description")
	}
	if !strings.Contains(out, "3 objects deleted successfully") {
		t.Error("output should confirm deletion")
	}
}

func TestDeleteByTagWithYes(t *testing.T) {
	out, err := executeCommand("delete", "--tag", "temporary", "-y")
	if err != nil {
		t.Fatalf("delete --tag with -y should succeed: %v", err)
	}
	if !strings.Contains(out, "Tag: temporary") {
		t.Error("output should show tag filter")
	}
}

func TestDeleteByMentionWithYes(t *testing.T) {
	out, err := executeCommand("delete", "--mention", "@project.archived", "-y")
	if err != nil {
		t.Fatalf("delete --mention with -y should succeed: %v", err)
	}
	if !strings.Contains(out, "Mention: @project.archived") {
		t.Error("output should show mention filter")
	}
}

func TestDeleteAllWithYes(t *testing.T) {
	out, err := executeCommand("delete", "--all", "-y")
	if err != nil {
		t.Fatalf("delete --all with -y should succeed: %v", err)
	}
	if !strings.Contains(out, "ALL objects") {
		t.Error("output should indicate all objects")
	}
}

func TestDeleteNoFilterError(t *testing.T) {
	_, err := executeCommand("delete")
	if err == nil {
		t.Error("delete without any filter should fail")
	}
}

func TestDeleteHelp(t *testing.T) {
	out, err := executeCommand("delete", "--help")
	if err != nil {
		t.Fatalf("delete --help should succeed: %v", err)
	}
	for _, flag := range []string{"--id", "--tag", "--hint", "--mention", "--type", "--subtype", "--all", "-y"} {
		if !strings.Contains(out, flag) {
			t.Errorf("delete help should list flag %s", flag)
		}
	}
}

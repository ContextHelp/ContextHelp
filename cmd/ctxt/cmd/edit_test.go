package cmd

import (
	"strings"
	"testing"
)

func TestEditTitle(t *testing.T) {
	out, err := executeCommand("edit", "--id", "obj_123", "--title", "New title")
	if err != nil {
		t.Fatalf("edit with --title should succeed: %v", err)
	}
	if !strings.Contains(out, "Updating knowledge object: obj_123") {
		t.Error("output should show object ID")
	}
	if !strings.Contains(out, "updated successfully") {
		t.Error("output should confirm update")
	}
}

func TestEditMultipleFields(t *testing.T) {
	out, err := executeCommand("edit", "--id", "obj_123", "--title", "New title", "--tags", "ux,design", "--subtype", "article")
	if err != nil {
		t.Fatalf("edit with multiple fields should succeed: %v", err)
	}
	if !strings.Contains(out, "Changes:") {
		t.Error("output should list changes")
	}
}

func TestEditNoFieldsError(t *testing.T) {
	_, err := executeCommand("edit", "--id", "obj_123")
	if err == nil {
		t.Error("edit without any fields should fail")
	}
}

func TestEditMissingIDError(t *testing.T) {
	_, err := executeCommand("edit", "--title", "New title")
	if err == nil {
		t.Error("edit without --id should fail")
	}
}

func TestEditHelp(t *testing.T) {
	out, err := executeCommand("edit", "--help")
	if err != nil {
		t.Fatalf("edit --help should succeed: %v", err)
	}
	for _, flag := range []string{"--id", "--title", "--summary", "--tags", "--hints", "--mentions", "--decisions", "--subtype"} {
		if !strings.Contains(out, flag) {
			t.Errorf("edit help should list flag %s", flag)
		}
	}
}

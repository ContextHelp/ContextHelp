package cmd

import (
	"strings"
	"testing"
)

func TestOpen(t *testing.T) {
	out, err := executeCommand("open", "obj_12345678")
	if err != nil {
		t.Fatalf("open should succeed: %v", err)
	}
	if !strings.Contains(out, "Knowledge Object: obj_12345678") {
		t.Error("output should show object ID")
	}
	if !strings.Contains(out, "Title:") {
		t.Error("output should contain Title field")
	}
	if !strings.Contains(out, "Tags:") {
		t.Error("output should contain Tags field")
	}
	if !strings.Contains(out, "Mentions:") {
		t.Error("output should contain Mentions field")
	}
	if !strings.Contains(out, "Summary:") {
		t.Error("output should contain Summary section")
	}
	if !strings.Contains(out, "Decisions:") {
		t.Error("output should contain Decisions section")
	}
}

func TestOpenNoIDError(t *testing.T) {
	_, err := executeCommand("open")
	if err == nil {
		t.Error("open without ID should fail")
	}
}

func TestOpenHelp(t *testing.T) {
	out, err := executeCommand("open", "--help")
	if err != nil {
		t.Fatalf("open --help should succeed: %v", err)
	}
	if !strings.Contains(out, "--raw") {
		t.Error("open help should list --raw flag")
	}
}

package cmd

import (
	"strings"
	"testing"
)

func TestURIHelp(t *testing.T) {
	out, err := executeCommand("uri", "--help")
	if err != nil {
		t.Fatalf("uri --help should succeed: %v", err)
	}
	if !strings.Contains(out, "register") {
		t.Error("uri help should mention register subcommand")
	}
	if !strings.Contains(out, "snippet") {
		t.Error("uri help should mention snippet subcommand")
	}
}

func TestURIRegisterHelp(t *testing.T) {
	out, err := executeCommand("uri", "register", "--help")
	if err != nil {
		t.Fatalf("uri register --help should succeed: %v", err)
	}
	if !strings.Contains(out, "ctxt://") {
		t.Error("uri register help should mention ctxt://")
	}
}

func TestURISnippetDarwin(t *testing.T) {
	out, err := executeCommand("uri", "snippet", "--platform", "macos")
	if err != nil {
		t.Fatalf("uri snippet --platform macos should succeed: %v", err)
	}
	if len(strings.TrimSpace(out)) == 0 {
		t.Error("snippet output should not be empty")
	}
}

func TestURISnippetLinux(t *testing.T) {
	out, err := executeCommand("uri", "snippet", "--platform", "linux")
	if err != nil {
		t.Fatalf("uri snippet --platform linux should succeed: %v", err)
	}
	if !strings.Contains(out, "ctxt") {
		t.Error("linux snippet should mention ctxt")
	}
}

func TestURISnippetWindows(t *testing.T) {
	out, err := executeCommand("uri", "snippet", "--platform", "windows")
	if err != nil {
		t.Fatalf("uri snippet --platform windows should succeed: %v", err)
	}
	if !strings.Contains(out, "ctxt") {
		t.Error("windows snippet should mention ctxt")
	}
}

func TestURISnippetUnknownPlatform(t *testing.T) {
	_, err := executeCommand("uri", "snippet", "--platform", "haiku")
	if err == nil {
		t.Error("unknown platform should return error")
	}
}

func TestDispatchURIObject(t *testing.T) {
	db := setupTestDB(t)
	// Use ctxt://obj_00000000 — not found, but routing should reach runOpen
	// (which returns an error about not finding the object, not a routing error).
	_, err := db.exec("ctxt://obj_00000000")
	if err == nil {
		t.Error("ctxt:// dispatch to non-existent object should error")
	}
	// Error should be from the open/storage layer, not a routing error.
	if strings.Contains(err.Error(), "invalid ctxt:// URI") {
		t.Errorf("should not be a URI parse error, got: %v", err)
	}
	if strings.Contains(err.Error(), "empty object ID") {
		t.Errorf("should not be an empty ID error, got: %v", err)
	}
}

func TestRootSubcommandsIncludesURI(t *testing.T) {
	out, err := executeCommand("--help")
	if err != nil {
		t.Fatalf("root --help should succeed: %v", err)
	}
	if !strings.Contains(out, "uri") {
		t.Error("root help should list uri subcommand")
	}
}

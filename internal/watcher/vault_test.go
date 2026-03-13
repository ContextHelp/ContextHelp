package watcher_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/watcher"
)

func TestParseFile_Generic(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "note.md")
	os.WriteFile(p, []byte("# Hello World"), 0644)

	req, err := watcher.ParseFile(p, "generic", dir)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if req.Content == "" {
		t.Error("expected non-empty content")
	}
	if req.Type != "text" {
		t.Errorf("type: got %q, want text", req.Type)
	}
}

func TestParseFile_Obsidian(t *testing.T) {
	dir := t.TempDir()
	os.Mkdir(filepath.Join(dir, ".obsidian"), 0755)
	p := filepath.Join(dir, "note.md")
	os.WriteFile(p, []byte("---\ntitle: Test Note\n---\n\n# Test Note\n\nContent here."), 0644)

	req, err := watcher.ParseFile(p, "obsidian", dir)
	if err != nil {
		t.Fatalf("ParseFile (obsidian): %v", err)
	}
	if req.Content == "" {
		t.Error("expected content from obsidian parse")
	}
}

func TestParseFile_Logseq(t *testing.T) {
	dir := t.TempDir()
	os.Mkdir(filepath.Join(dir, "logseq"), 0755)
	p := filepath.Join(dir, "pages", "my-page.md")
	os.MkdirAll(filepath.Dir(p), 0755)
	os.WriteFile(p, []byte("- Block one\n  - Nested block\n"), 0644)

	req, err := watcher.ParseFile(p, "logseq", dir)
	if err != nil {
		t.Fatalf("ParseFile (logseq): %v", err)
	}
	if req.Content == "" {
		t.Error("expected content from logseq parse")
	}
}

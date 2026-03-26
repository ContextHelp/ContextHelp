package watcher_test

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/watcher"
)

func TestSemanticWatcherHooks_ExtractMentionHints_Empty(t *testing.T) {
	h := watcher.NewSemanticWatcherHooks()
	hints := h.ExtractMentionHints("")
	if len(hints) != 0 {
		t.Errorf("expected 0 hints for empty string, got %d", len(hints))
	}
}

func TestSemanticWatcherHooks_ExtractMentionHints_NoMentions(t *testing.T) {
	h := watcher.NewSemanticWatcherHooks()
	hints := h.ExtractMentionHints("just some plain text without mentions")
	if len(hints) != 0 {
		t.Errorf("expected 0 hints, got %d", len(hints))
	}
}

func TestSemanticWatcherHooks_ExtractMentionHints_ValidMention(t *testing.T) {
	h := watcher.NewSemanticWatcherHooks()
	hints := h.ExtractMentionHints("see @react.server-components for details")
	if len(hints) != 1 {
		t.Fatalf("expected 1 hint, got %d", len(hints))
	}
	if hints[0].Raw != "@react.server-components" {
		t.Errorf("expected @react.server-components, got %q", hints[0].Raw)
	}
}

func TestSemanticWatcherHooks_ExtractMentionHints_MultipleMentions(t *testing.T) {
	h := watcher.NewSemanticWatcherHooks()
	hints := h.ExtractMentionHints("@react.hooks and @typescript.generics are both relevant")
	if len(hints) != 2 {
		t.Fatalf("expected 2 hints, got %d: %v", len(hints), hints)
	}
}

func TestSemanticWatcherHooks_ExtractMentionHints_Deduplicates(t *testing.T) {
	h := watcher.NewSemanticWatcherHooks()
	hints := h.ExtractMentionHints("@react.hooks and also @react.hooks again")
	if len(hints) != 1 {
		t.Errorf("expected 1 deduplicated hint, got %d", len(hints))
	}
}

func TestSemanticWatcherHooks_ExtractMentionHints_RequiresDot(t *testing.T) {
	h := watcher.NewSemanticWatcherHooks()
	// @react alone (no dot) should be rejected.
	hints := h.ExtractMentionHints("see @react for details")
	if len(hints) != 0 {
		t.Errorf("expected 0 hints for mention without dot, got %d", len(hints))
	}
}

func TestSemanticWatcherHooks_OnFileDiscovered(t *testing.T) {
	h := watcher.NewSemanticWatcherHooks()
	hints := h.OnFileDiscovered(
		"/notes/react.server-components.md",
		"# Notes on @typescript.generics",
	)
	// Should find @typescript.generics from metadata.
	found := false
	for _, hint := range hints {
		if hint.Raw == "@typescript.generics" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected @typescript.generics in hints from metadata, got: %v", hints)
	}
}

func TestSemanticWatcherHooks_OnFileDiscovered_NoMentions(t *testing.T) {
	h := watcher.NewSemanticWatcherHooks()
	hints := h.OnFileDiscovered("/notes/plain-file.md", "no mentions here")
	if len(hints) != 0 {
		t.Errorf("expected 0 hints for plain path, got %d", len(hints))
	}
}

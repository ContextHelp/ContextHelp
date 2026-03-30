package steps

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
)

func TestLanguageDetectorFromSource(t *testing.T) {
	step := NewLanguageDetector()
	draft := &storage.KnowledgeObject{
		Source: "/home/user/main.go",
	}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got.Metadata["language"] != "go" {
		t.Errorf("language: %v", got.Metadata["language"])
	}
}

func TestLanguageDetectorFromMetadataFilename(t *testing.T) {
	step := NewLanguageDetector()
	draft := &storage.KnowledgeObject{
		Metadata: map[string]any{
			"filename": "script.py",
		},
	}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got.Metadata["language"] != "python" {
		t.Errorf("language: %v", got.Metadata["language"])
	}
}

func TestLanguageDetectorUnknownExtension(t *testing.T) {
	step := NewLanguageDetector()
	draft := &storage.KnowledgeObject{
		Source: "/tmp/file.xyz",
	}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if _, ok := got.Metadata["language"]; ok {
		t.Error("expected no language for unknown extension")
	}
}

func TestLanguageDetectorName(t *testing.T) {
	step := NewLanguageDetector()
	if step.Name() != "language_detector" {
		t.Errorf("name: %q", step.Name())
	}
}

func TestLanguageDetectorGraphNodeEmitted(t *testing.T) {
	step := NewLanguageDetector()
	draft := &storage.KnowledgeObject{
		ID:     "obj-010",
		Source: "/project/main.go",
	}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got.Graph == nil {
		t.Fatal("Graph must not be nil when language detected with non-empty ID")
	}
	if len(got.Graph.Nodes) != 1 {
		t.Fatalf("Graph.Nodes: got %d, want 1", len(got.Graph.Nodes))
	}
	n := got.Graph.Nodes[0]
	if n.NodeType != pluginapi.NodeTypeTag {
		t.Errorf("NodeType: got %q, want %q", n.NodeType, pluginapi.NodeTypeTag)
	}
	if n.Label != "lang:go" {
		t.Errorf("Label: got %q, want %q", n.Label, "lang:go")
	}
	if n.Metadata["source"] != "language_detector" {
		t.Errorf("Metadata source: got %v", n.Metadata["source"])
	}
	if n.Metadata["lang"] != "go" {
		t.Errorf("Metadata lang: got %v", n.Metadata["lang"])
	}
}

func TestLanguageDetectorGraphSkippedWithEmptyID(t *testing.T) {
	step := NewLanguageDetector()
	draft := &storage.KnowledgeObject{
		// ID intentionally empty.
		Source: "/project/main.go",
	}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got.Graph != nil {
		t.Errorf("Graph should be nil when draft.ID is empty, got %+v", got.Graph)
	}
}

func TestLanguageDetectorGraphSkippedUnknownExtension(t *testing.T) {
	step := NewLanguageDetector()
	draft := &storage.KnowledgeObject{
		ID:     "obj-011",
		Source: "/project/file.xyz",
	}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got.Graph != nil {
		t.Errorf("Graph should be nil for unknown extension, got %+v", got.Graph)
	}
}

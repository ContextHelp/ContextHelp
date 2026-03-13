package steps

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
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

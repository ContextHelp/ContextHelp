package steps

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestFunctionExtractorGoFunctions(t *testing.T) {
	step := NewFunctionExtractor()
	draft := &storage.KnowledgeObject{
		RawContent: "package main\n\nfunc Hello(name string) string {\n\treturn \"Hello \" + name\n}\n\nfunc (s *Server) Run() error {\n\treturn nil\n}",
		Metadata:   map[string]any{"language": "go"},
	}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got.Metadata["function_count"] != 2 {
		t.Errorf("function_count: %v", got.Metadata["function_count"])
	}
	if len(got.Sections) != 2 {
		t.Errorf("expected 2 sections, got %d", len(got.Sections))
	}
	for _, sec := range got.Sections {
		if sec.Metadata["type"] != "function" {
			t.Errorf("section type: %v", sec.Metadata["type"])
		}
	}
}

func TestFunctionExtractorSkipsNonGo(t *testing.T) {
	step := NewFunctionExtractor()
	draft := &storage.KnowledgeObject{
		RawContent: "def hello():\n    pass",
		Metadata:   map[string]any{"language": "python"},
	}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(got.Sections) != 0 {
		t.Errorf("expected 0 sections for non-Go, got %d", len(got.Sections))
	}
}

func TestFunctionExtractorName(t *testing.T) {
	step := NewFunctionExtractor()
	if step.Name() != "function_extractor" {
		t.Errorf("name: %q", step.Name())
	}
}

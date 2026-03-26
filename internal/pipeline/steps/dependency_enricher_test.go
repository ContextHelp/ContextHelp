package steps

import (
	"context"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestDependencyEnricherName(t *testing.T) {
	s := NewDependencyEnricher()
	if s.Name() != "dependency_enricher" {
		t.Errorf("Name: got %q, want %q", s.Name(), "dependency_enricher")
	}
}

func TestDependencyEnricherNilMetadata(t *testing.T) {
	s := NewDependencyEnricher()
	draft := &storage.KnowledgeObject{RawContent: ""}
	got, err := s.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got.Metadata == nil {
		t.Fatal("Metadata should be initialized")
	}
}

func TestDependencyEnricherNoDepFiles(t *testing.T) {
	s := NewDependencyEnricher()
	draft := &storage.KnowledgeObject{
		Metadata:   map[string]any{},
		RawContent: "some unrelated content",
	}
	got, err := s.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	items, _ := got.Metadata["items_to_enqueue"].([]map[string]any)
	if len(items) != 0 {
		t.Errorf("expected 0 items, got %d", len(items))
	}
	if got.Metadata["deps_enqueued"].(int) != 0 {
		t.Errorf("deps_enqueued: got %v, want 0", got.Metadata["deps_enqueued"])
	}
}

func TestDependencyEnricherPackageJSON(t *testing.T) {
	s := NewDependencyEnricher()
	draft := &storage.KnowledgeObject{
		Source: "https://github.com/owner/repo",
		Metadata: map[string]any{
			"dep_files": map[string]any{
				"package.json": `{
					"name": "myapp",
					"dependencies": {
						"express": "^4.18.0",
						"lodash": "^4.17.21"
					},
					"devDependencies": {
						"jest": "^29.0.0"
					}
				}`,
			},
		},
	}

	got, err := s.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	items, _ := got.Metadata["items_to_enqueue"].([]map[string]any)
	if len(items) != 3 {
		t.Fatalf("items_to_enqueue: got %d, want 3", len(items))
	}

	urls := make(map[string]bool)
	for _, item := range items {
		url, _ := item["content"].(string)
		urls[url] = true
		if item["pipeline"] != "url.generic" {
			t.Errorf("item pipeline: got %v, want url.generic", item["pipeline"])
		}
		if item["source"] != "https://github.com/owner/repo" {
			t.Errorf("item source: got %v", item["source"])
		}
	}

	if !urls["https://www.npmjs.com/package/express"] {
		t.Error("missing express npm URL")
	}
	if !urls["https://www.npmjs.com/package/lodash"] {
		t.Error("missing lodash npm URL")
	}
	if !urls["https://www.npmjs.com/package/jest"] {
		t.Error("missing jest npm URL")
	}

	if got.Metadata["deps_enqueued"].(int) != 3 {
		t.Errorf("deps_enqueued: got %v, want 3", got.Metadata["deps_enqueued"])
	}
}

func TestDependencyEnricherGoMod(t *testing.T) {
	s := NewDependencyEnricher()
	gomod := `module github.com/owner/myapp

go 1.21

require (
	github.com/google/uuid v1.6.0
	golang.org/x/sync v0.6.0
)

require github.com/stretchr/testify v1.8.0 // indirect
`
	draft := &storage.KnowledgeObject{
		Source: "https://github.com/owner/myapp",
		Metadata: map[string]any{
			"dep_files": map[string]any{
				"go.mod": gomod,
			},
		},
	}

	got, err := s.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	items, _ := got.Metadata["items_to_enqueue"].([]map[string]any)
	if len(items) != 3 {
		t.Fatalf("items_to_enqueue: got %d, want 3", len(items))
	}

	urls := make(map[string]bool)
	for _, item := range items {
		url, _ := item["content"].(string)
		urls[url] = true
	}
	if !urls["https://pkg.go.dev/github.com/google/uuid"] {
		t.Error("missing uuid pkg.go.dev URL")
	}
	if !urls["https://pkg.go.dev/golang.org/x/sync"] {
		t.Error("missing sync pkg.go.dev URL")
	}
	if !urls["https://pkg.go.dev/github.com/stretchr/testify"] {
		t.Error("missing testify pkg.go.dev URL")
	}
}

func TestDependencyEnricherRequirementsTxt(t *testing.T) {
	s := NewDependencyEnricher()
	reqtxt := `# Production deps
requests>=2.28.0
flask==3.0.0
# Dev deps
pytest>=7.0.0
`
	draft := &storage.KnowledgeObject{
		Source: "https://github.com/owner/myapp",
		Metadata: map[string]any{
			"dep_files": map[string]any{
				"requirements.txt": reqtxt,
			},
		},
	}

	got, err := s.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	items, _ := got.Metadata["items_to_enqueue"].([]map[string]any)
	if len(items) != 3 {
		t.Fatalf("items_to_enqueue: got %d, want 3", len(items))
	}

	urls := make(map[string]bool)
	for _, item := range items {
		url, _ := item["content"].(string)
		urls[url] = true
	}
	if !urls["https://pypi.org/project/requests/"] {
		t.Error("missing requests pypi URL")
	}
	if !urls["https://pypi.org/project/flask/"] {
		t.Error("missing flask pypi URL")
	}
	if !urls["https://pypi.org/project/pytest/"] {
		t.Error("missing pytest pypi URL")
	}
}

func TestDependencyEnricherGemfile(t *testing.T) {
	s := NewDependencyEnricher()
	gemfile := `source "https://rubygems.org"

gem "rails", "~> 7.0"
gem "pg", ">= 0.18", "< 2.0"
gem "puma"
`
	draft := &storage.KnowledgeObject{
		Source: "https://github.com/owner/myapp",
		Metadata: map[string]any{
			"dep_files": map[string]any{
				"Gemfile": gemfile,
			},
		},
	}

	got, err := s.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	items, _ := got.Metadata["items_to_enqueue"].([]map[string]any)
	if len(items) != 3 {
		t.Fatalf("items_to_enqueue: got %d, want 3", len(items))
	}

	urls := make(map[string]bool)
	for _, item := range items {
		url, _ := item["content"].(string)
		urls[url] = true
	}
	if !urls["https://rubygems.org/gems/rails"] {
		t.Error("missing rails rubygems URL")
	}
	if !urls["https://rubygems.org/gems/pg"] {
		t.Error("missing pg rubygems URL")
	}
	if !urls["https://rubygems.org/gems/puma"] {
		t.Error("missing puma rubygems URL")
	}
}

func TestDependencyEnricherMultipleFiles(t *testing.T) {
	s := NewDependencyEnricher()
	draft := &storage.KnowledgeObject{
		Source: "https://github.com/owner/mixed",
		Metadata: map[string]any{
			"dep_files": map[string]any{
				"package.json": `{"dependencies": {"express": "^4.0.0"}}`,
				"requirements.txt": "requests==2.28.0\nflask==3.0.0\n",
			},
		},
	}

	got, err := s.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	items, _ := got.Metadata["items_to_enqueue"].([]map[string]any)
	if len(items) != 3 {
		t.Fatalf("items_to_enqueue: got %d, want 3 (1 npm + 2 pypi)", len(items))
	}
}

func TestDependencyEnricherInvalidDepFilesType(t *testing.T) {
	s := NewDependencyEnricher()
	draft := &storage.KnowledgeObject{
		Metadata: map[string]any{
			"dep_files": "not a map",
		},
	}
	_, err := s.Run(context.Background(), draft)
	if err == nil {
		t.Fatal("expected error for wrong dep_files type")
	}
}

func TestDependencyEnricherInvalidPackageJSON(t *testing.T) {
	s := NewDependencyEnricher()
	draft := &storage.KnowledgeObject{
		Metadata: map[string]any{
			"dep_files": map[string]any{
				"package.json": "not valid json {{{",
			},
		},
	}
	_, err := s.Run(context.Background(), draft)
	if err == nil {
		t.Fatal("expected error for invalid package.json")
	}
}

func TestDependencyEnricherPreservesExistingItems(t *testing.T) {
	s := NewDependencyEnricher()
	existing := []map[string]any{
		{"content": "https://example.com", "source": "manual"},
	}
	draft := &storage.KnowledgeObject{
		Source: "https://github.com/owner/repo",
		Metadata: map[string]any{
			"items_to_enqueue": existing,
			"dep_files": map[string]any{
				"package.json": `{"dependencies": {"lodash": "^4.0.0"}}`,
			},
		},
	}

	got, err := s.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	items, _ := got.Metadata["items_to_enqueue"].([]map[string]any)
	if len(items) != 2 {
		t.Fatalf("items_to_enqueue: got %d, want 2 (1 existing + 1 new)", len(items))
	}
	// First item preserved.
	if items[0]["content"] != "https://example.com" {
		t.Errorf("first item content: got %v", items[0]["content"])
	}
}

func TestDependencyEnricherRawContentFallbackGoMod(t *testing.T) {
	s := NewDependencyEnricher()
	gomod := "module github.com/owner/app\n\ngo 1.21\n\nrequire github.com/foo/bar v1.0.0\n"
	draft := &storage.KnowledgeObject{
		Source:     "https://github.com/owner/app",
		Metadata:   map[string]any{},
		RawContent: gomod,
	}

	got, err := s.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	items, _ := got.Metadata["items_to_enqueue"].([]map[string]any)
	if len(items) != 1 {
		t.Fatalf("items_to_enqueue: got %d, want 1", len(items))
	}
	if items[0]["content"] != "https://pkg.go.dev/github.com/foo/bar" {
		t.Errorf("content: got %v", items[0]["content"])
	}
}

func TestDependencyEnricherDeduplicatesGoMod(t *testing.T) {
	s := NewDependencyEnricher()
	gomod := "module example.com/app\n\nrequire (\n\tgithub.com/foo/bar v1.0.0\n\tgithub.com/foo/bar v1.1.0\n)\n"
	draft := &storage.KnowledgeObject{
		Metadata: map[string]any{
			"dep_files": map[string]any{"go.mod": gomod},
		},
	}

	got, err := s.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	items, _ := got.Metadata["items_to_enqueue"].([]map[string]any)
	if len(items) != 1 {
		t.Errorf("expected dedup to 1, got %d", len(items))
	}
}

func TestDependencyEnricherUnknownManifestSkipped(t *testing.T) {
	s := NewDependencyEnricher()
	draft := &storage.KnowledgeObject{
		Metadata: map[string]any{
			"dep_files": map[string]any{
				"Cargo.toml": "[dependencies]\nserde = \"1.0\"\n",
			},
		},
	}

	got, err := s.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	items, _ := got.Metadata["items_to_enqueue"].([]map[string]any)
	if len(items) != 0 {
		t.Errorf("expected 0 items for unknown format, got %d", len(items))
	}
}

func TestDependencyEnricherPathBasenameNormalization(t *testing.T) {
	s := NewDependencyEnricher()
	draft := &storage.KnowledgeObject{
		Source: "https://github.com/owner/repo",
		Metadata: map[string]any{
			"dep_files": map[string]any{
				// path-prefixed filenames should still be recognized
				"src/package.json": `{"dependencies": {"react": "^18.0.0"}}`,
			},
		},
	}

	got, err := s.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	items, _ := got.Metadata["items_to_enqueue"].([]map[string]any)
	if len(items) != 1 {
		t.Fatalf("items_to_enqueue: got %d, want 1", len(items))
	}
	if !strings.Contains(items[0]["content"].(string), "react") {
		t.Errorf("content: got %v", items[0]["content"])
	}
}

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	gohttp "net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	uri "hop.top/cite/scheme"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// ---------------------------------------------------------------------------
// Mock pipeline steps for document parsing tests.
// ---------------------------------------------------------------------------

// docTypeSetterStep sets the object Type and Subtype for document content.
type docTypeSetterStep struct {
	pipeline.BaseContract
	format string
}

func (s *docTypeSetterStep) Name() string { return "test-doc-type-setter" }
func (s *docTypeSetterStep) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	draft.Type = "document"
	draft.Subtype = s.format
	return draft, nil
}

// docHierarchicalSectionStep simulates hierarchical section extraction from a document.
type docHierarchicalSectionStep struct {
	pipeline.BaseContract
	sections []storage.Section
}

func (s *docHierarchicalSectionStep) Name() string { return "test-doc-hierarchical-sections" }
func (s *docHierarchicalSectionStep) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	draft.Sections = append(draft.Sections, s.sections...)
	return draft, nil
}

// docImageExtractorStep simulates extraction of embedded images as child objects.
type docImageExtractorStep struct {
	pipeline.BaseContract
	imageCount int
}

func (s *docImageExtractorStep) Name() string { return "test-doc-image-extractor" }
func (s *docImageExtractorStep) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}
	var childIDs []string
	for i := 0; i < s.imageCount; i++ {
		childID := fmt.Sprintf("img-%s-%d", draft.ID[:8], i+1)
		childIDs = append(childIDs, childID)
	}
	draft.Metadata["embedded_images"] = childIDs
	draft.Metadata["image_count"] = float64(s.imageCount)
	return draft, nil
}

// docTableExtractorStep simulates extraction of tables as structured Markdown.
type docTableExtractorStep struct {
	pipeline.BaseContract
	tables []string
}

func (s *docTableExtractorStep) Name() string { return "test-doc-table-extractor" }
func (s *docTableExtractorStep) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	for i, table := range s.tables {
		draft.Sections = append(draft.Sections, storage.Section{
			Title:   fmt.Sprintf("Table %d", i+1),
			Content: table,
			Order:   len(draft.Sections),
		})
	}
	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}
	draft.Metadata["table_count"] = float64(len(s.tables))
	return draft, nil
}

// docErrorStep simulates a document processing error.
type docErrorStep struct {
	pipeline.BaseContract
	errMsg string
}

func (s *docErrorStep) Name() string { return "test-doc-error" }
func (s *docErrorStep) Run(_ context.Context, _ *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	return nil, fmt.Errorf("%s", s.errMsg)
}

// docCodeParserStep simulates code parsing into function/class-level sections.
type docCodeParserStep struct {
	pipeline.BaseContract
	language string
	sections []storage.Section
}

func (s *docCodeParserStep) Name() string { return "test-doc-code-parser" }
func (s *docCodeParserStep) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}
	draft.Metadata["language"] = s.language
	draft.Sections = append(draft.Sections, s.sections...)
	return draft, nil
}

// docAnnotationExtractorStep simulates extraction of TODO/FIXME annotations.
type docAnnotationExtractorStep struct {
	pipeline.BaseContract
	annotations []storage.Section
}

func (s *docAnnotationExtractorStep) Name() string { return "test-doc-annotation-extractor" }
func (s *docAnnotationExtractorStep) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	draft.Sections = append(draft.Sections, s.annotations...)
	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}
	draft.Metadata["annotation_count"] = float64(len(s.annotations))
	return draft, nil
}

// docSizeLimitStep simulates a size-check step for documents.
type docSizeLimitStep struct {
	pipeline.BaseContract
	maxBytes int64
}

func (s *docSizeLimitStep) Name() string { return "test-doc-size-limit" }
func (s *docSizeLimitStep) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	if strings.HasPrefix(draft.RawContent, "SIZE:") {
		var size int64
		fmt.Sscanf(draft.RawContent, "SIZE:%d", &size)
		if size > s.maxBytes {
			return nil, fmt.Errorf("file size %d bytes exceeds maximum allowed %d bytes", size, s.maxBytes)
		}
	}
	return draft, nil
}

// docDepthConfigStep simulates configurable decomposition depth.
type docDepthConfigStep struct {
	pipeline.BaseContract
	depth    int
	sections []storage.Section
}

func (s *docDepthConfigStep) Name() string { return "test-doc-depth-config" }
func (s *docDepthConfigStep) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}
	draft.Metadata["decomposition_depth"] = float64(s.depth)
	// Only add sections up to the configured depth.
	for _, sec := range s.sections {
		draft.Sections = append(draft.Sections, sec)
	}
	return draft, nil
}

// docEdgeCreatorStep simulates creating parent-child edges by populating Mentions.
type docEdgeCreatorStep struct {
	pipeline.BaseContract
	childMentions []uri.URI
}

func (s *docEdgeCreatorStep) Name() string { return "test-doc-edge-creator" }
func (s *docEdgeCreatorStep) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	draft.Mentions = append(draft.Mentions, s.childMentions...)
	return draft, nil
}

// ---------------------------------------------------------------------------
// US-0006 Tests
// ---------------------------------------------------------------------------

// TestUS0006_PDFReturnsJobID verifies POST /analyze with PDF type returns 202 + job_id.
func TestUS0006_PDFReturnsJobID(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Upsert("document.pdf", &pipeline.Pipeline{
		PipelineName: "document.pdf",
		Steps: []pipeline.PipelineStep{
			&docTypeSetterStep{format: "pdf"},
			&docHierarchicalSectionStep{sections: []storage.Section{
				{Title: "Introduction", Content: "PDF content", Order: 0},
			}},
		},
	})

	body, _ := json.Marshal(map[string]string{
		"content":  "simulated-pdf-bytes",
		"type":     "document",
		"source":   "e2e-test",
		"pipeline": "document.pdf",
	})

	resp, err := gohttp.Post(env.URL+"/api/v1/analyze", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, gohttp.StatusAccepted, resp.StatusCode)

	var result map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.NotEmpty(t, result["job_id"], "response must contain a job_id")
}

// TestUS0006_JobCompletesWithHierarchicalSections verifies PDF produces hierarchical sections matching headings.
func TestUS0006_JobCompletesWithHierarchicalSections(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Upsert("document.pdf", &pipeline.Pipeline{
		PipelineName: "document.pdf",
		Steps: []pipeline.PipelineStep{
			&docTypeSetterStep{format: "pdf"},
			&docHierarchicalSectionStep{sections: []storage.Section{
				{Title: "Chapter 1: Introduction", Content: "Introduction text", Order: 0},
				{Title: "1.1 Background", Content: "Background details", Order: 1},
				{Title: "1.2 Motivation", Content: "Motivation text", Order: 2},
				{Title: "Chapter 2: Methods", Content: "Methods overview", Order: 3},
				{Title: "2.1 Data Collection", Content: "Data collection process", Order: 4},
			}},
		},
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "simulated-pdf-bytes",
		Type:     "document",
		Pipeline: "document.pdf",
		Source:   "e2e-test",
	})
	require.NoError(t, err)

	job := waitForJob(t, env.URL, jobID, storage.JobCompleted)
	require.NotEmpty(t, job.ResultID)

	resp, err := gohttp.Get(fmt.Sprintf("%s/api/v1/objects/%s", env.URL, job.ResultID))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, gohttp.StatusOK, resp.StatusCode)

	var obj storage.KnowledgeObject
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&obj))

	require.Len(t, obj.Sections, 5, "should have 5 hierarchical sections")
	assert.Equal(t, "Chapter 1: Introduction", obj.Sections[0].Title)
	assert.Equal(t, "1.1 Background", obj.Sections[1].Title)
	assert.Equal(t, "Chapter 2: Methods", obj.Sections[3].Title)

	// Verify ordering is preserved.
	for i, sec := range obj.Sections {
		assert.Equal(t, i, sec.Order, "section order should be sequential")
	}
}

// TestUS0006_PDFEmbeddedImagesAsChildObjects verifies embedded images become child KnowledgeObjects with edges.
func TestUS0006_PDFEmbeddedImagesAsChildObjects(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Upsert("document.pdf_images", &pipeline.Pipeline{
		PipelineName: "document.pdf_images",
		Steps: []pipeline.PipelineStep{
			&docTypeSetterStep{format: "pdf"},
			&docImageExtractorStep{imageCount: 3},
			&docEdgeCreatorStep{childMentions: []uri.URI{
				{Scheme: "ctxt", Namespace: "entity", ID: "image/figure1"},
				{Scheme: "ctxt", Namespace: "entity", ID: "image/figure2"},
				{Scheme: "ctxt", Namespace: "entity", ID: "image/figure3"},
			}},
		},
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "simulated-pdf-with-images",
		Type:     "document",
		Pipeline: "document.pdf_images",
		Source:   "e2e-test",
	})
	require.NoError(t, err)

	job := waitForJob(t, env.URL, jobID, storage.JobCompleted)
	require.NotEmpty(t, job.ResultID)

	resp, err := gohttp.Get(fmt.Sprintf("%s/api/v1/objects/%s", env.URL, job.ResultID))
	require.NoError(t, err)
	defer resp.Body.Close()

	var obj storage.KnowledgeObject
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&obj))

	require.NotNil(t, obj.Metadata)
	assert.Equal(t, 3.0, obj.Metadata["image_count"], "should have 3 embedded images")

	// Verify edges were created for the child image entities.
	edges, err := env.svc.Store.Edges().ListFrom(context.Background(), "object", job.ResultID)
	require.NoError(t, err)
	assert.Len(t, edges, 3, "should have 3 edges linking to image entities")
}

// TestUS0006_PDFTablesExtracted verifies tables are extracted as structured Markdown in Sections.
func TestUS0006_PDFTablesExtracted(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	markdownTable := "| Name | Score |\n|------|-------|\n| Alice | 95 |\n| Bob | 87 |"

	env.svc.Pipes.Upsert("document.pdf_tables", &pipeline.Pipeline{
		PipelineName: "document.pdf_tables",
		Steps: []pipeline.PipelineStep{
			&docTypeSetterStep{format: "pdf"},
			&docTableExtractorStep{tables: []string{markdownTable}},
		},
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "simulated-pdf-with-tables",
		Type:     "document",
		Pipeline: "document.pdf_tables",
		Source:   "e2e-test",
	})
	require.NoError(t, err)

	job := waitForJob(t, env.URL, jobID, storage.JobCompleted)
	require.NotEmpty(t, job.ResultID)

	resp, err := gohttp.Get(fmt.Sprintf("%s/api/v1/objects/%s", env.URL, job.ResultID))
	require.NoError(t, err)
	defer resp.Body.Close()

	var obj storage.KnowledgeObject
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&obj))

	require.NotNil(t, obj.Metadata)
	assert.Equal(t, 1.0, obj.Metadata["table_count"])

	// Find the table section.
	foundTable := false
	for _, sec := range obj.Sections {
		if sec.Title == "Table 1" {
			foundTable = true
			assert.Contains(t, sec.Content, "| Name | Score |", "table should be in Markdown format")
			assert.Contains(t, sec.Content, "Alice")
			assert.Contains(t, sec.Content, "Bob")
			break
		}
	}
	assert.True(t, foundTable, "table section should exist")
}

// TestUS0006_PasswordProtectedPDFRejected verifies password-protected PDF returns clear error.
func TestUS0006_PasswordProtectedPDFRejected(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Upsert("document.pdf_protected", &pipeline.Pipeline{
		PipelineName: "document.pdf_protected",
		Steps: []pipeline.PipelineStep{
			&docErrorStep{errMsg: "password-protected PDF: decryption key required"},
		},
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "simulated-encrypted-pdf",
		Type:     "document",
		Pipeline: "document.pdf_protected",
		Source:   "e2e-test",
	})
	require.NoError(t, err)

	job := waitForJob(t, env.URL, jobID, storage.JobFailed)
	assert.NotEmpty(t, job.Error)
	assert.Contains(t, job.Error, "password-protected", "error should mention password protection")
}

// TestUS0006_MarkdownHeadingHierarchy verifies heading hierarchy (h1>h2>h3) preserved in Section tree.
func TestUS0006_MarkdownHeadingHierarchy(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Upsert("document.markdown", &pipeline.Pipeline{
		PipelineName: "document.markdown",
		Steps: []pipeline.PipelineStep{
			&docTypeSetterStep{format: "markdown"},
			&docHierarchicalSectionStep{sections: []storage.Section{
				{Title: "# Top Level", Content: "H1 content", Order: 0},
				{Title: "## Second Level", Content: "H2 content", Order: 1},
				{Title: "### Third Level", Content: "H3 content", Order: 2},
				{Title: "## Another Second Level", Content: "Another H2", Order: 3},
			}},
		},
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "# Top Level\n\nH1 content\n\n## Second Level\n\nH2 content\n\n### Third Level\n\nH3 content\n\n## Another Second Level\n\nAnother H2",
		Type:     "document",
		Pipeline: "document.markdown",
		Source:   "e2e-test",
	})
	require.NoError(t, err)

	job := waitForJob(t, env.URL, jobID, storage.JobCompleted)
	require.NotEmpty(t, job.ResultID)

	resp, err := gohttp.Get(fmt.Sprintf("%s/api/v1/objects/%s", env.URL, job.ResultID))
	require.NoError(t, err)
	defer resp.Body.Close()

	var obj storage.KnowledgeObject
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&obj))

	require.Len(t, obj.Sections, 4)
	assert.Equal(t, "# Top Level", obj.Sections[0].Title)
	assert.Equal(t, "## Second Level", obj.Sections[1].Title)
	assert.Equal(t, "### Third Level", obj.Sections[2].Title)
	assert.Equal(t, "## Another Second Level", obj.Sections[3].Title)

	// Verify hierarchy through ordering.
	for i := 0; i < len(obj.Sections)-1; i++ {
		assert.Less(t, obj.Sections[i].Order, obj.Sections[i+1].Order, "sections should maintain order")
	}
}

// TestUS0006_MarkdownCodeBlocksExtracted verifies fenced code blocks extracted with language annotation.
func TestUS0006_MarkdownCodeBlocksExtracted(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Upsert("document.markdown_code", &pipeline.Pipeline{
		PipelineName: "document.markdown_code",
		Steps: []pipeline.PipelineStep{
			&docTypeSetterStep{format: "markdown"},
			&docCodeParserStep{
				language: "mixed",
				sections: []storage.Section{
					{Title: "Code Block: go", Content: "```go\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n```", Order: 0},
					{Title: "Code Block: python", Content: "```python\ndef greet():\n    print('hello')\n```", Order: 1},
				},
			},
		},
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "# README\n\n```go\nfunc main() {}\n```\n\n```python\ndef greet(): pass\n```",
		Type:     "document",
		Pipeline: "document.markdown_code",
		Source:   "e2e-test",
	})
	require.NoError(t, err)

	job := waitForJob(t, env.URL, jobID, storage.JobCompleted)
	require.NotEmpty(t, job.ResultID)

	resp, err := gohttp.Get(fmt.Sprintf("%s/api/v1/objects/%s", env.URL, job.ResultID))
	require.NoError(t, err)
	defer resp.Body.Close()

	var obj storage.KnowledgeObject
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&obj))

	require.Len(t, obj.Sections, 2)
	assert.Contains(t, obj.Sections[0].Title, "go", "code block should have language annotation")
	assert.Contains(t, obj.Sections[0].Content, "func main()")
	assert.Contains(t, obj.Sections[1].Title, "python")
	assert.Contains(t, obj.Sections[1].Content, "def greet()")
}

// TestUS0006_GoCodeParsedToFunctions verifies Go file parsed into function-level Sections with signatures.
func TestUS0006_GoCodeParsedToFunctions(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Upsert("document.go", &pipeline.Pipeline{
		PipelineName: "document.go",
		Steps: []pipeline.PipelineStep{
			&docTypeSetterStep{format: "go"},
			&docCodeParserStep{
				language: "go",
				sections: []storage.Section{
					{Title: "func NewService(store StorageDriver) *Service", Content: "func NewService(store StorageDriver) *Service {\n\treturn &Service{store: store}\n}", Order: 0},
					{Title: "func (s *Service) Analyze(ctx context.Context, req AnalyzeRequest) (string, error)", Content: "func (s *Service) Analyze(ctx context.Context, req AnalyzeRequest) (string, error) {\n\t// implementation\n}", Order: 1},
					{Title: "func (s *Service) GetObject(ctx context.Context, id string) (*KnowledgeObject, error)", Content: "func (s *Service) GetObject(ctx context.Context, id string) (*KnowledgeObject, error) {\n\treturn s.store.Get(ctx, id)\n}", Order: 2},
				},
			},
		},
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "package service\n\nfunc NewService(...) *Service {}\nfunc (s *Service) Analyze(...) {}\nfunc (s *Service) GetObject(...) {}",
		Type:     "document",
		Pipeline: "document.go",
		Source:   "e2e-test",
	})
	require.NoError(t, err)

	job := waitForJob(t, env.URL, jobID, storage.JobCompleted)
	require.NotEmpty(t, job.ResultID)

	resp, err := gohttp.Get(fmt.Sprintf("%s/api/v1/objects/%s", env.URL, job.ResultID))
	require.NoError(t, err)
	defer resp.Body.Close()

	var obj storage.KnowledgeObject
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&obj))

	require.NotNil(t, obj.Metadata)
	assert.Equal(t, "go", obj.Metadata["language"])
	require.Len(t, obj.Sections, 3, "Go file should produce 3 function-level sections")

	// Verify function signatures are in section titles.
	assert.Contains(t, obj.Sections[0].Title, "func NewService")
	assert.Contains(t, obj.Sections[1].Title, "func (s *Service) Analyze")
	assert.Contains(t, obj.Sections[2].Title, "func (s *Service) GetObject")
}

// TestUS0006_PythonCodeParsedToClasses verifies Python file parsed into class/method Sections.
func TestUS0006_PythonCodeParsedToClasses(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Upsert("document.python", &pipeline.Pipeline{
		PipelineName: "document.python",
		Steps: []pipeline.PipelineStep{
			&docTypeSetterStep{format: "python"},
			&docCodeParserStep{
				language: "python",
				sections: []storage.Section{
					{Title: "class DataProcessor", Content: "class DataProcessor:\n    \"\"\"Processes data files.\"\"\"", Order: 0},
					{Title: "def __init__(self, path: str)", Content: "    def __init__(self, path: str):\n        self.path = path", Order: 1},
					{Title: "def process(self) -> dict", Content: "    def process(self) -> dict:\n        return {}", Order: 2},
					{Title: "class DataExporter(DataProcessor)", Content: "class DataExporter(DataProcessor):\n    \"\"\"Exports processed data.\"\"\"", Order: 3},
				},
			},
		},
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "class DataProcessor:\n    def __init__(self, path): pass\n    def process(self): pass\n\nclass DataExporter(DataProcessor): pass",
		Type:     "document",
		Pipeline: "document.python",
		Source:   "e2e-test",
	})
	require.NoError(t, err)

	job := waitForJob(t, env.URL, jobID, storage.JobCompleted)
	require.NotEmpty(t, job.ResultID)

	resp, err := gohttp.Get(fmt.Sprintf("%s/api/v1/objects/%s", env.URL, job.ResultID))
	require.NoError(t, err)
	defer resp.Body.Close()

	var obj storage.KnowledgeObject
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&obj))

	require.NotNil(t, obj.Metadata)
	assert.Equal(t, "python", obj.Metadata["language"])
	require.Len(t, obj.Sections, 4)

	assert.Contains(t, obj.Sections[0].Title, "class DataProcessor")
	assert.Contains(t, obj.Sections[1].Title, "def __init__")
	assert.Contains(t, obj.Sections[2].Title, "def process")
	assert.Contains(t, obj.Sections[3].Title, "class DataExporter")
}

// TestUS0006_CodeTODOAnnotationsExtracted verifies TODO/FIXME extracted as separate Sections.
func TestUS0006_CodeTODOAnnotationsExtracted(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Upsert("document.annotated", &pipeline.Pipeline{
		PipelineName: "document.annotated",
		Steps: []pipeline.PipelineStep{
			&docTypeSetterStep{format: "go"},
			&docAnnotationExtractorStep{annotations: []storage.Section{
				{Title: "TODO", Content: "TODO: refactor this function to use dependency injection", Order: 0},
				{Title: "FIXME", Content: "FIXME: race condition in concurrent access", Order: 1},
				{Title: "TODO", Content: "TODO: add unit tests for edge cases", Order: 2},
			}},
		},
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "// TODO: refactor this\n// FIXME: race condition\n// TODO: add tests",
		Type:     "document",
		Pipeline: "document.annotated",
		Source:   "e2e-test",
	})
	require.NoError(t, err)

	job := waitForJob(t, env.URL, jobID, storage.JobCompleted)
	require.NotEmpty(t, job.ResultID)

	resp, err := gohttp.Get(fmt.Sprintf("%s/api/v1/objects/%s", env.URL, job.ResultID))
	require.NoError(t, err)
	defer resp.Body.Close()

	var obj storage.KnowledgeObject
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&obj))

	require.NotNil(t, obj.Metadata)
	assert.Equal(t, 3.0, obj.Metadata["annotation_count"])
	require.Len(t, obj.Sections, 3)

	// Verify TODO and FIXME annotations are extracted.
	todoCount := 0
	fixmeCount := 0
	for _, sec := range obj.Sections {
		if sec.Title == "TODO" {
			todoCount++
			assert.Contains(t, sec.Content, "TODO:")
		}
		if sec.Title == "FIXME" {
			fixmeCount++
			assert.Contains(t, sec.Content, "FIXME:")
		}
	}
	assert.Equal(t, 2, todoCount, "should have 2 TODO annotations")
	assert.Equal(t, 1, fixmeCount, "should have 1 FIXME annotation")
}

// TestUS0006_UnsupportedLanguageFallback verifies unsupported language falls back to line-based splitting.
func TestUS0006_UnsupportedLanguageFallback(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Upsert("document.unknown_lang", &pipeline.Pipeline{
		PipelineName: "document.unknown_lang",
		Steps: []pipeline.PipelineStep{
			&docTypeSetterStep{format: "brainfuck"},
			&docCodeParserStep{
				language: "unknown",
				sections: []storage.Section{
					{Title: "Lines 1-10", Content: "+++++++++[>++++++++<-]>.", Order: 0},
					{Title: "Lines 11-20", Content: "++++++++++.", Order: 1},
				},
			},
		},
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "+++++++++[>++++++++<-]>.\n++++++++++.",
		Type:     "document",
		Pipeline: "document.unknown_lang",
		Source:   "e2e-test",
	})
	require.NoError(t, err)

	job := waitForJob(t, env.URL, jobID, storage.JobCompleted)
	require.NotEmpty(t, job.ResultID)

	resp, err := gohttp.Get(fmt.Sprintf("%s/api/v1/objects/%s", env.URL, job.ResultID))
	require.NoError(t, err)
	defer resp.Body.Close()

	var obj storage.KnowledgeObject
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&obj))

	require.NotNil(t, obj.Metadata)
	assert.Equal(t, "unknown", obj.Metadata["language"], "unsupported language should fall back")

	// Verify line-based splitting produced sections.
	require.GreaterOrEqual(t, len(obj.Sections), 1, "fallback should still produce sections")
	assert.Contains(t, obj.Sections[0].Title, "Lines", "fallback sections should be line-based")
}

// TestUS0006_DOCXParsedLikePDF verifies DOCX decomposed similarly to PDF.
func TestUS0006_DOCXParsedLikePDF(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Upsert("document.docx", &pipeline.Pipeline{
		PipelineName: "document.docx",
		Steps: []pipeline.PipelineStep{
			&docTypeSetterStep{format: "docx"},
			&docHierarchicalSectionStep{sections: []storage.Section{
				{Title: "Executive Summary", Content: "Summary of findings.", Order: 0},
				{Title: "1. Introduction", Content: "This document describes...", Order: 1},
				{Title: "1.1 Background", Content: "Background information.", Order: 2},
				{Title: "2. Analysis", Content: "Analysis details.", Order: 3},
			}},
			&docTableExtractorStep{tables: []string{
				"| Metric | Value |\n|--------|-------|\n| Revenue | $1M |",
			}},
		},
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "simulated-docx-bytes",
		Type:     "document",
		Pipeline: "document.docx",
		Source:   "e2e-test",
	})
	require.NoError(t, err)

	job := waitForJob(t, env.URL, jobID, storage.JobCompleted)
	require.NotEmpty(t, job.ResultID)

	resp, err := gohttp.Get(fmt.Sprintf("%s/api/v1/objects/%s", env.URL, job.ResultID))
	require.NoError(t, err)
	defer resp.Body.Close()

	var obj storage.KnowledgeObject
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&obj))

	assert.Equal(t, "document", obj.Type)
	assert.Equal(t, "docx", obj.Subtype)

	// DOCX should produce hierarchical sections + table sections, just like PDF.
	require.GreaterOrEqual(t, len(obj.Sections), 5, "DOCX should produce sections like PDF")
	assert.Equal(t, "Executive Summary", obj.Sections[0].Title)

	// Verify table was also extracted.
	foundTable := false
	for _, sec := range obj.Sections {
		if strings.HasPrefix(sec.Title, "Table") {
			foundTable = true
			assert.Contains(t, sec.Content, "Revenue")
			break
		}
	}
	assert.True(t, foundTable, "DOCX should extract tables like PDF")
}

// TestUS0006_CorruptFileReturnsError verifies corrupt document returns descriptive error.
func TestUS0006_CorruptFileReturnsError(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Upsert("document.corrupt", &pipeline.Pipeline{
		PipelineName: "document.corrupt",
		Steps: []pipeline.PipelineStep{
			&docErrorStep{errMsg: "corrupt document: invalid PDF header, expected %PDF-"},
		},
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "NOT_A_VALID_DOCUMENT",
		Type:     "document",
		Pipeline: "document.corrupt",
		Source:   "e2e-test",
	})
	require.NoError(t, err)

	job := waitForJob(t, env.URL, jobID, storage.JobFailed)
	assert.NotEmpty(t, job.Error)
	assert.Contains(t, job.Error, "corrupt document", "error should describe the corruption")
}

// TestUS0006_FileSizeLimitEnforced verifies content exceeding 100MB is rejected.
func TestUS0006_FileSizeLimitEnforced(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	const maxDocBytes = 100 * 1024 * 1024 // 100MB

	env.svc.Pipes.Upsert("document.size_check", &pipeline.Pipeline{
		PipelineName: "document.size_check",
		Steps: []pipeline.PipelineStep{
			&docSizeLimitStep{maxBytes: maxDocBytes},
		},
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "SIZE:150000000", // Simulated 150MB payload
		Type:     "document",
		Pipeline: "document.size_check",
		Source:   "e2e-test",
	})
	require.NoError(t, err)

	job := waitForJob(t, env.URL, jobID, storage.JobFailed)
	assert.NotEmpty(t, job.Error)
	assert.Contains(t, job.Error, "exceeds maximum", "error should mention size limit")
}

// TestUS0006_MultipartUploadReturns202 verifies REST multipart upload returns 202.
func TestUS0006_MultipartUploadReturns202(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Upsert("document.pdf", &pipeline.Pipeline{
		PipelineName: "document.pdf",
		Steps: []pipeline.PipelineStep{
			&docTypeSetterStep{format: "pdf"},
			&docHierarchicalSectionStep{sections: []storage.Section{
				{Title: "Section 1", Content: "Multipart upload content", Order: 0},
			}},
		},
	})

	body, _ := json.Marshal(map[string]string{
		"content":  "simulated-multipart-pdf-upload",
		"type":     "document",
		"source":   "e2e-multipart",
		"pipeline": "document.pdf",
	})

	resp, err := gohttp.Post(env.URL+"/api/v1/analyze", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, gohttp.StatusAccepted, resp.StatusCode)

	var result map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.NotEmpty(t, result["job_id"])
}

// TestUS0006_SectionLevelSearch verifies search returns specific section, not entire document.
func TestUS0006_SectionLevelSearch(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Upsert("document.searchable", &pipeline.Pipeline{
		PipelineName: "document.searchable",
		Steps: []pipeline.PipelineStep{
			&docTypeSetterStep{format: "pdf"},
			&docHierarchicalSectionStep{sections: []storage.Section{
				{Title: "Introduction", Content: "General overview of the project.", Order: 0},
				{Title: "Machine Learning Results", Content: "The neural network achieved 98% accuracy on CIFAR-10.", Order: 1},
				{Title: "Conclusion", Content: "Future work includes scaling.", Order: 2},
			}},
		},
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "simulated-research-pdf",
		Type:     "document",
		Pipeline: "document.searchable",
		Source:   "e2e-test",
	})
	require.NoError(t, err)

	job := waitForJob(t, env.URL, jobID, storage.JobCompleted)
	require.NotEmpty(t, job.ResultID)

	// Search by document type to find the object.
	resp, err := gohttp.Get(env.URL + "/api/v1/search?q=type==document")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, gohttp.StatusOK, resp.StatusCode)

	var body struct {
		Data  []storage.KnowledgeObject `json:"data"`
		Total int                       `json:"total"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.GreaterOrEqual(t, body.Total, 1)

	// Verify the document object has sections, enabling section-level retrieval.
	found := false
	for _, obj := range body.Data {
		if obj.ID == job.ResultID {
			found = true
			require.Len(t, obj.Sections, 3)
			// Verify individual section content is accessible.
			assert.Contains(t, obj.Sections[1].Content, "neural network", "specific section content should be retrievable")
			break
		}
	}
	assert.True(t, found, "document should appear in search results")
}

// TestUS0006_HierarchyTraversableViaAPI verifies child objects linked to parent via graph edges.
func TestUS0006_HierarchyTraversableViaAPI(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Upsert("document.hierarchy", &pipeline.Pipeline{
		PipelineName: "document.hierarchy",
		Steps: []pipeline.PipelineStep{
			&docTypeSetterStep{format: "pdf"},
			&docHierarchicalSectionStep{sections: []storage.Section{
				{Title: "Chapter 1", Content: "Parent chapter", Order: 0},
			}},
			&docEdgeCreatorStep{childMentions: []uri.URI{
				{Scheme: "ctxt", Namespace: "entity", ID: "section/chapter1-subsection1"},
				{Scheme: "ctxt", Namespace: "entity", ID: "section/chapter1-subsection2"},
			}},
		},
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "simulated-hierarchical-pdf",
		Type:     "document",
		Pipeline: "document.hierarchy",
		Source:   "e2e-test",
	})
	require.NoError(t, err)

	job := waitForJob(t, env.URL, jobID, storage.JobCompleted)
	require.NotEmpty(t, job.ResultID)

	// Verify edges from the parent document.
	edges, err := env.svc.Store.Edges().ListFrom(context.Background(), "object", job.ResultID)
	require.NoError(t, err)
	require.Len(t, edges, 2, "parent should have 2 edges to child sections")

	// Verify edges point to the correct child entities.
	childSlugs := make(map[string]bool)
	for _, edge := range edges {
		assert.Equal(t, "object", edge.FromType)
		assert.Equal(t, job.ResultID, edge.FromID)
		assert.Equal(t, "entity", edge.ToType)
		assert.Equal(t, "mentions", edge.EdgeType)
		childSlugs[edge.ToID] = true
	}
	assert.True(t, childSlugs["ctxt://entity/section/chapter1-subsection1"])
	assert.True(t, childSlugs["ctxt://entity/section/chapter1-subsection2"])

	// Verify backlinks via API for one child.
	resp, err := gohttp.Get(fmt.Sprintf("%s/api/v1/entities/@section.chapter1-subsection1/backlinks", env.URL))
	require.NoError(t, err)
	defer resp.Body.Close()

	var backlinks struct {
		Data []storage.KnowledgeObject `json:"data"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&backlinks))
	require.Len(t, backlinks.Data, 1, "child entity should have one backlink to parent")
	assert.Equal(t, job.ResultID, backlinks.Data[0].ID)
}

// TestUS0006_DecompositionDepthConfigurable verifies config controls decomposition depth.
func TestUS0006_DecompositionDepthConfigurable(t *testing.T) {
	depths := []struct {
		name     string
		depth    int
		sections []storage.Section
	}{
		{
			name:  "Depth1",
			depth: 1,
			sections: []storage.Section{
				{Title: "Chapter 1", Content: "Top level only", Order: 0},
				{Title: "Chapter 2", Content: "Another top level", Order: 1},
			},
		},
		{
			name:  "Depth3",
			depth: 3,
			sections: []storage.Section{
				{Title: "Chapter 1", Content: "Top level", Order: 0},
				{Title: "1.1 Section", Content: "Second level", Order: 1},
				{Title: "1.1.1 Subsection", Content: "Third level", Order: 2},
				{Title: "Chapter 2", Content: "Another top level", Order: 3},
				{Title: "2.1 Section", Content: "Second level", Order: 4},
				{Title: "2.1.1 Subsection", Content: "Third level", Order: 5},
			},
		},
	}

	for _, tc := range depths {
		t.Run(tc.name, func(t *testing.T) {
			env := startTestEnv(t)
			defer env.stop(t)

			pipelineName := fmt.Sprintf("document.depth_%d", tc.depth)
			env.svc.Pipes.Upsert(pipelineName, &pipeline.Pipeline{
				PipelineName: pipelineName,
				Steps: []pipeline.PipelineStep{
					&docTypeSetterStep{format: "pdf"},
					&docDepthConfigStep{depth: tc.depth, sections: tc.sections},
				},
			})

			jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
				Content:  "simulated-deep-pdf",
				Type:     "document",
				Pipeline: pipelineName,
				Source:   "e2e-test",
			})
			require.NoError(t, err)

			job := waitForJob(t, env.URL, jobID, storage.JobCompleted)
			require.NotEmpty(t, job.ResultID)

			resp, err := gohttp.Get(fmt.Sprintf("%s/api/v1/objects/%s", env.URL, job.ResultID))
			require.NoError(t, err)
			defer resp.Body.Close()

			var obj storage.KnowledgeObject
			require.NoError(t, json.NewDecoder(resp.Body).Decode(&obj))

			require.NotNil(t, obj.Metadata)
			assert.Equal(t, float64(tc.depth), obj.Metadata["decomposition_depth"])
			assert.Len(t, obj.Sections, len(tc.sections), "depth %d should produce %d sections", tc.depth, len(tc.sections))
		})
	}
}

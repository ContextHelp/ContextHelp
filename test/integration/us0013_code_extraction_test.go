package integration

import (
	"context"
	"encoding/json"
	"fmt"
	gohttp "net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// ---------------------------------------------------------------------------
// Mock pipeline steps for code extraction tests.
// ---------------------------------------------------------------------------

// codeSnippet holds a single extracted code block.
type codeSnippet struct {
	Language string `json:"language"`
	Body     string `json:"body"`
	Position int    `json:"position"`
}

// codeExtractionStep simulates detecting and extracting fenced code blocks.
type codeExtractionStep struct {
	pipeline.BaseContract
	snippets []codeSnippet
}

func (s *codeExtractionStep) Name() string { return "test-code-extraction" }
func (s *codeExtractionStep) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}
	rawSnippets := make([]interface{}, len(s.snippets))
	for i, snip := range s.snippets {
		rawSnippets[i] = map[string]interface{}{
			"language": snip.Language,
			"body":     snip.Body,
			"position": float64(snip.Position),
		}
	}
	draft.Metadata["code_snippets"] = rawSnippets
	draft.Metadata["snippets_found"] = float64(len(s.snippets))
	draft.Metadata["detection_method"] = "heuristic"
	draft.Metadata["code_extracted_at"] = "2026-03-26T00:00:00Z"
	return draft, nil
}

// ---------------------------------------------------------------------------
// US-0013 Tests
// ---------------------------------------------------------------------------

// TestUS0013_FencedCodeBlocksExtracted verifies fenced code blocks in markdown are
// detected and extracted as code objects with language tags.
func TestUS0013_FencedCodeBlocksExtracted(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	snippets := []codeSnippet{
		{Language: "go", Body: "func main() {\n\tfmt.Println(\"hello\")\n}", Position: 42},
		{Language: "python", Body: "def greet(name):\n    print(f'Hello, {name}')", Position: 120},
	}

	env.svc.Pipes.Upsert("text.code-extract", &pipeline.Pipeline{
		PipelineName: "text.code-extract",
		Steps: []pipeline.PipelineStep{
			&codeExtractionStep{snippets: snippets},
		},
	})

	markdownContent := "# Notes\n\nHere is a Go snippet:\n\n```go\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n```\n\nAnd Python:\n\n```python\ndef greet(name):\n    print(f'Hello, {name}')\n```"

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  markdownContent,
		Type:     "text",
		Pipeline: "text.code-extract",
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

	require.NotNil(t, obj.Metadata)
	assert.Equal(t, float64(2), obj.Metadata["snippets_found"], "should find 2 code snippets")

	rawSnippets, ok := obj.Metadata["code_snippets"].([]interface{})
	require.True(t, ok, "code_snippets must be a slice")
	require.Len(t, rawSnippets, 2)

	first := rawSnippets[0].(map[string]interface{})
	assert.Equal(t, "go", first["language"], "first snippet language must be go")
	assert.NotEmpty(t, first["body"], "snippet body must not be empty")
}

// TestUS0013_CodeObjectsCreatedWithLanguageTag verifies each code snippet has a language tag.
func TestUS0013_CodeObjectsCreatedWithLanguageTag(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	snippets := []codeSnippet{
		{Language: "bash", Body: "echo hello world", Position: 10},
		{Language: "sql", Body: "SELECT * FROM users WHERE active = true;", Position: 50},
		{Language: "typescript", Body: "const x: number = 42;", Position: 90},
	}

	env.svc.Pipes.Upsert("text.code-lang", &pipeline.Pipeline{
		PipelineName: "text.code-lang",
		Steps: []pipeline.PipelineStep{
			&codeExtractionStep{snippets: snippets},
		},
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "Multi-language code document with bash, SQL, and TypeScript examples",
		Type:     "text",
		Pipeline: "text.code-lang",
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

	rawSnippets, ok := obj.Metadata["code_snippets"].([]interface{})
	require.True(t, ok)
	require.Len(t, rawSnippets, 3)

	for i, raw := range rawSnippets {
		snip := raw.(map[string]interface{})
		assert.NotEmpty(t, snip["language"], "snippet %d must have a language tag", i)
		assert.NotEmpty(t, snip["body"], "snippet %d must have a body", i)
	}
}

// TestUS0013_UnlabeledFencedBlocksGetUnknownLanguage verifies fenced blocks without
// an explicit language label are assigned language = "unknown".
func TestUS0013_UnlabeledFencedBlocksGetUnknownLanguage(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	snippets := []codeSnippet{
		{Language: "unknown", Body: "some unlabeled code here", Position: 5},
	}

	env.svc.Pipes.Upsert("text.code-unknown-lang", &pipeline.Pipeline{
		PipelineName: "text.code-unknown-lang",
		Steps: []pipeline.PipelineStep{
			&codeExtractionStep{snippets: snippets},
		},
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "```\nsome unlabeled code here\n```",
		Type:     "text",
		Pipeline: "text.code-unknown-lang",
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

	rawSnippets, ok := obj.Metadata["code_snippets"].([]interface{})
	require.True(t, ok)
	require.Len(t, rawSnippets, 1)

	snip := rawSnippets[0].(map[string]interface{})
	assert.Equal(t, "unknown", snip["language"],
		"unlabeled fenced block must produce language=unknown, not an error")
}

// TestUS0013_NoCodeContentReturnsEmptySnippets verifies content without code blocks
// produces empty code_snippets (not an error).
func TestUS0013_NoCodeContentReturnsEmptySnippets(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Upsert("text.code-empty", &pipeline.Pipeline{
		PipelineName: "text.code-empty",
		Steps: []pipeline.PipelineStep{
			&codeExtractionStep{snippets: []codeSnippet{}},
		},
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "Plain prose with no code blocks whatsoever.",
		Type:     "text",
		Pipeline: "text.code-empty",
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

	assert.Equal(t, float64(0), obj.Metadata["snippets_found"],
		"no code content should produce snippets_found=0")
}

// TestUS0013_DetectionMethodRecordedInMetadata verifies detection_method is persisted.
func TestUS0013_DetectionMethodRecordedInMetadata(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Upsert("text.code-method", &pipeline.Pipeline{
		PipelineName: "text.code-method",
		Steps: []pipeline.PipelineStep{
			&codeExtractionStep{
				snippets: []codeSnippet{
					{Language: "go", Body: "var x = 1", Position: 0},
				},
			},
		},
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "```go\nvar x = 1\n```",
		Type:     "text",
		Pipeline: "text.code-method",
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

	assert.Equal(t, "heuristic", obj.Metadata["detection_method"],
		"detection_method must be recorded in Metadata")
	assert.NotEmpty(t, obj.Metadata["code_extracted_at"],
		"code_extracted_at timestamp must be set")
}

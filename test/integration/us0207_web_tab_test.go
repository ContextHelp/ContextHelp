package integration

// US-0207: Browser Tab and Element Capture
//
// Verifies: mock browser extension message, capture tab content,
// page text/title/url stored on resulting object.
// All external HTTP calls use httptest.NewServer — no real network.
// Gate: INTEGRATION=1 env var required.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	gohttp "net/http"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// ---------------------------------------------------------------------------
// Simulated browser extension message payload
// ---------------------------------------------------------------------------

// extensionMessage represents the payload sent by the browser extension.
type extensionMessage struct {
	CaptureMode string `json:"capture_mode"` // "full_page", "selection", "element"
	SourceURL   string `json:"source_url"`
	PageTitle   string `json:"page_title"`
	HTMLContent string `json:"html_content,omitempty"`
	TextContent string `json:"text_content,omitempty"`
	CapturedAt  string `json:"captured_at"`
	AuthState   string `json:"auth_state"` // "authenticated" | "anonymous"
}

// ---------------------------------------------------------------------------
// Pipeline steps
// ---------------------------------------------------------------------------

// webTabExtractStep simulates readability extraction on pre-fetched HTML.
type webTabExtractStep struct {
	pipeline.BaseContract
}

func (s *webTabExtractStep) Name() string { return "test-web-tab-extract" }
func (s *webTabExtractStep) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}

	// RawContent is the JSON-serialized extension message.
	var msg extensionMessage
	if err := json.Unmarshal([]byte(draft.RawContent), &msg); err != nil {
		return draft, err
	}

	draft.Type = "web.page"
	if msg.CaptureMode == "selection" {
		draft.Type = "web.selection"
	} else if msg.CaptureMode == "element" {
		draft.Type = "web.element"
	}

	draft.TextContent = msg.TextContent
	draft.Source = msg.SourceURL
	draft.Metadata["page_title"] = msg.PageTitle
	draft.Metadata["source_url"] = msg.SourceURL
	draft.Metadata["capture_mode"] = msg.CaptureMode
	draft.Metadata["captured_at"] = msg.CapturedAt
	draft.Metadata["auth_state"] = msg.AuthState
	return draft, nil
}

// ---------------------------------------------------------------------------
// US-0207 Tests
// ---------------------------------------------------------------------------

// TestUS0207_FullPageCapture verifies full-page capture stores page text, title,
// and source URL.
func TestUS0207_FullPageCapture(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("set INTEGRATION=1")
	}

	const pageTitle = "How Distributed Systems Work"
	const pageText = "Distributed systems require coordination across multiple nodes."
	const pageURL = "https://example.com/distributed-systems"

	// Simulate the browser extension payload.
	msg := extensionMessage{
		CaptureMode: "full_page",
		SourceURL:   pageURL,
		PageTitle:   pageTitle,
		TextContent: pageText,
		CapturedAt:  "2026-03-01T12:00:00Z",
		AuthState:   "anonymous",
	}
	msgJSON, _ := json.Marshal(msg)

	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Upsert("web.page", &pipeline.Pipeline{
		PipelineName: "web.page",
		Steps: []pipeline.PipelineStep{
			&webTabExtractStep{},
		},
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  string(msgJSON),
		Type:     "url",
		Pipeline: "web.page",
		Source:   pageURL,
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

	assert.Equal(t, "web.page", obj.Type)
	assert.Equal(t, pageText, obj.TextContent, "page text must be stored")
	assert.Equal(t, pageURL, obj.Source, "source URL must be stored")
	assert.Equal(t, pageTitle, obj.Metadata["page_title"], "page title must be stored")
}

// TestUS0207_SelectionCapture verifies selection capture routes to web.selection pipeline
// and stores selected text with source URL.
func TestUS0207_SelectionCapture(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("set INTEGRATION=1")
	}

	const selectedText = "consensus algorithms are fundamental to distributed systems"
	const pageURL = "https://example.com/consensus"

	msg := extensionMessage{
		CaptureMode: "selection",
		SourceURL:   pageURL,
		PageTitle:   "Consensus in Distributed Systems",
		TextContent: selectedText,
		CapturedAt:  "2026-03-01T12:05:00Z",
		AuthState:   "anonymous",
	}
	msgJSON, _ := json.Marshal(msg)

	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Upsert("web.selection", &pipeline.Pipeline{
		PipelineName: "web.selection",
		Steps: []pipeline.PipelineStep{
			&webTabExtractStep{},
		},
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  string(msgJSON),
		Type:     "text",
		Pipeline: "web.selection",
		Source:   pageURL,
	})
	require.NoError(t, err)

	job := waitForJob(t, env.URL, jobID, storage.JobCompleted)
	require.NotEmpty(t, job.ResultID)

	resp, err := gohttp.Get(fmt.Sprintf("%s/api/v1/objects/%s", env.URL, job.ResultID))
	require.NoError(t, err)
	defer resp.Body.Close()

	var obj storage.KnowledgeObject
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&obj))

	assert.Equal(t, "web.selection", obj.Type)
	assert.Equal(t, selectedText, obj.TextContent)
	assert.Equal(t, pageURL, obj.Source)
}

// TestUS0207_CaptureViaAnalyzeAPIReturnsJobID verifies the extension's POST to
// /api/v1/analyze returns a job_id.
func TestUS0207_CaptureViaAnalyzeAPIReturnsJobID(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("set INTEGRATION=1")
	}

	env := startTestEnv(t)
	defer env.stop(t)

	msg := extensionMessage{
		CaptureMode: "full_page",
		SourceURL:   "https://example.com/article",
		PageTitle:   "Test Article",
		TextContent: "Article content here.",
		CapturedAt:  "2026-03-01T12:10:00Z",
		AuthState:   "authenticated",
	}
	msgJSON, _ := json.Marshal(msg)

	body, _ := json.Marshal(map[string]string{
		"content":  string(msgJSON),
		"type":     "url",
		"pipeline": "web.page",
		"source":   "https://example.com/article",
	})

	resp, err := gohttp.Post(env.URL+"/api/v1/analyze", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, gohttp.StatusAccepted, resp.StatusCode)

	var result map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.NotEmpty(t, result["job_id"], "job_id must be returned immediately")
}

// TestUS0207_AuthStateStoredInMetadata verifies the authentication state from
// the extension message is stored in object metadata.
func TestUS0207_AuthStateStoredInMetadata(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("set INTEGRATION=1")
	}

	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Upsert("web.page", &pipeline.Pipeline{
		PipelineName: "web.page",
		Steps: []pipeline.PipelineStep{
			&webTabExtractStep{},
		},
	})

	msg := extensionMessage{
		CaptureMode: "full_page",
		SourceURL:   "https://paywalled.com/article",
		PageTitle:   "Premium Article",
		TextContent: "Premium content body.",
		CapturedAt:  "2026-03-01T12:15:00Z",
		AuthState:   "authenticated",
	}
	msgJSON, _ := json.Marshal(msg)

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  string(msgJSON),
		Type:     "url",
		Pipeline: "web.page",
		Source:   "https://paywalled.com/article",
	})
	require.NoError(t, err)

	job := waitForJob(t, env.URL, jobID, storage.JobCompleted)
	require.NotEmpty(t, job.ResultID)

	resp, err := gohttp.Get(fmt.Sprintf("%s/api/v1/objects/%s", env.URL, job.ResultID))
	require.NoError(t, err)
	defer resp.Body.Close()

	var obj storage.KnowledgeObject
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&obj))

	assert.Equal(t, "authenticated", obj.Metadata["auth_state"],
		"auth state from extension message must be stored in metadata")
}

package integration

// US-0205: Wikipedia Article Capture
//
// Verifies: mock Wikipedia API, capture article,
// title/summary/infobox entities extracted.
// All external HTTP calls use httptest.NewServer — no real network.
// Gate: INTEGRATION=1 env var required.

import (
	"context"
	"encoding/json"
	"fmt"
	gohttp "net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// ---------------------------------------------------------------------------
// Mock Wikipedia API payloads
// ---------------------------------------------------------------------------

type mockWikipediaSummary struct {
	Title   string `json:"title"`
	Extract string `json:"extract"`
	PageID  int    `json:"pageid"`
}

type mockWikipediaInfoboxEntity struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type mockWikipediaArticle struct {
	Title    string                       `json:"title"`
	Summary  string                       `json:"summary"`
	Infobox  []mockWikipediaInfoboxEntity `json:"infobox"`
	Sections []string                     `json:"sections"`
	URL      string                       `json:"url"`
}

// ---------------------------------------------------------------------------
// Pipeline step
// ---------------------------------------------------------------------------

// wikipediaFetchStep calls the mock Wikipedia REST API.
type wikipediaFetchStep struct {
	pipeline.BaseContract
	apiBaseURL string
	pageTitle  string
}

func (s *wikipediaFetchStep) Name() string { return "test-wikipedia-fetch" }
func (s *wikipediaFetchStep) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	// Fetch summary.
	summaryURL := fmt.Sprintf("%s/api/rest_v1/page/summary/%s", s.apiBaseURL, s.pageTitle)
	resp, err := gohttp.Get(summaryURL)
	if err != nil {
		return draft, err
	}
	defer resp.Body.Close()

	var summary mockWikipediaSummary
	if err := json.NewDecoder(resp.Body).Decode(&summary); err != nil {
		return draft, err
	}

	// Fetch enriched article data (infobox).
	articleURL := fmt.Sprintf("%s/api/rest_v1/page/article/%s", s.apiBaseURL, s.pageTitle)
	respArt, err := gohttp.Get(articleURL)
	if err != nil {
		return draft, err
	}
	defer respArt.Body.Close()

	var article mockWikipediaArticle
	if err := json.NewDecoder(respArt.Body).Decode(&article); err != nil {
		return draft, err
	}

	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}

	draft.Type = "reference.article"
	draft.TextContent = summary.Extract
	draft.Metadata["title"] = summary.Title
	draft.Metadata["summary"] = summary.Extract
	draft.Metadata["page_id"] = summary.PageID
	draft.Metadata["source"] = fmt.Sprintf("https://en.wikipedia.org/wiki/%s", s.pageTitle)

	// Store infobox entities as a list of name/value pairs.
	infoboxList := make([]map[string]string, len(article.Infobox))
	for i, ent := range article.Infobox {
		infoboxList[i] = map[string]string{"name": ent.Name, "value": ent.Value}
	}
	draft.Metadata["infobox"] = infoboxList

	return draft, nil
}

// ---------------------------------------------------------------------------
// US-0205 Tests
// ---------------------------------------------------------------------------

// TestUS0205_TitleSummaryStored verifies Wikipedia article title and summary
// are stored on the captured KnowledgeObject.
func TestUS0205_TitleSummaryStored(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("set INTEGRATION=1")
	}

	const pageTitle = "Distributed_computing"
	const wantTitle = "Distributed computing"
	const wantSummary = "Distributed computing is a field of computer science that studies distributed systems."

	srv := httptest.NewServer(gohttp.HandlerFunc(func(w gohttp.ResponseWriter, r *gohttp.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == fmt.Sprintf("/api/rest_v1/page/summary/%s", pageTitle):
			json.NewEncoder(w).Encode(mockWikipediaSummary{
				Title:   wantTitle,
				Extract: wantSummary,
				PageID:  8071,
			})
		case r.URL.Path == fmt.Sprintf("/api/rest_v1/page/article/%s", pageTitle):
			json.NewEncoder(w).Encode(mockWikipediaArticle{
				Title:   wantTitle,
				Summary: wantSummary,
				Infobox: []mockWikipediaInfoboxEntity{
					{Name: "discipline", Value: "Computer science"},
				},
			})
		default:
			w.WriteHeader(gohttp.StatusNotFound)
		}
	}))
	defer srv.Close()

	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Upsert("reference.wikipedia", &pipeline.Pipeline{
		PipelineName: "reference.wikipedia",
		Steps: []pipeline.PipelineStep{
			&wikipediaFetchStep{apiBaseURL: srv.URL, pageTitle: pageTitle},
		},
	})

	sourceURL := fmt.Sprintf("https://en.wikipedia.org/wiki/%s", pageTitle)
	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  sourceURL,
		Type:     "url",
		Pipeline: "reference.wikipedia",
		Source:   sourceURL,
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

	assert.Equal(t, "reference.article", obj.Type)
	assert.Equal(t, wantTitle, obj.Metadata["title"], "title must be stored")
	assert.Equal(t, wantSummary, obj.Metadata["summary"], "summary must be stored")
}

// TestUS0205_InfoboxEntitiesExtracted verifies infobox key/value entities are
// extracted and stored.
func TestUS0205_InfoboxEntitiesExtracted(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("set INTEGRATION=1")
	}

	const pageTitle = "Go_programming_language"

	srv := httptest.NewServer(gohttp.HandlerFunc(func(w gohttp.ResponseWriter, r *gohttp.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == fmt.Sprintf("/api/rest_v1/page/summary/%s", pageTitle):
			json.NewEncoder(w).Encode(mockWikipediaSummary{
				Title:   "Go (programming language)",
				Extract: "Go is a statically typed compiled language.",
				PageID:  25039021,
			})
		case r.URL.Path == fmt.Sprintf("/api/rest_v1/page/article/%s", pageTitle):
			json.NewEncoder(w).Encode(mockWikipediaArticle{
				Title:   "Go (programming language)",
				Summary: "Go is a statically typed compiled language.",
				Infobox: []mockWikipediaInfoboxEntity{
					{Name: "developer", Value: "Google"},
					{Name: "first_appeared", Value: "2009"},
					{Name: "paradigm", Value: "multi-paradigm"},
				},
			})
		default:
			w.WriteHeader(gohttp.StatusNotFound)
		}
	}))
	defer srv.Close()

	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Upsert("reference.wikipedia", &pipeline.Pipeline{
		PipelineName: "reference.wikipedia",
		Steps: []pipeline.PipelineStep{
			&wikipediaFetchStep{apiBaseURL: srv.URL, pageTitle: pageTitle},
		},
	})

	sourceURL := fmt.Sprintf("https://en.wikipedia.org/wiki/%s", pageTitle)
	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  sourceURL,
		Type:     "url",
		Pipeline: "reference.wikipedia",
		Source:   sourceURL,
	})
	require.NoError(t, err)

	job := waitForJob(t, env.URL, jobID, storage.JobCompleted)
	require.NotEmpty(t, job.ResultID)

	resp, err := gohttp.Get(fmt.Sprintf("%s/api/v1/objects/%s", env.URL, job.ResultID))
	require.NoError(t, err)
	defer resp.Body.Close()

	var obj storage.KnowledgeObject
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&obj))

	infobox, ok := obj.Metadata["infobox"].([]any)
	require.True(t, ok, "infobox must be a list")
	require.NotEmpty(t, infobox, "infobox must have entries")

	// Verify at least the developer entry is present.
	found := false
	for _, entry := range infobox {
		if m, ok := entry.(map[string]any); ok {
			if m["name"] == "developer" && m["value"] == "Google" {
				found = true
				break
			}
		}
	}
	assert.True(t, found, "infobox must contain developer=Google entity")
}

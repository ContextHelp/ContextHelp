package integration

// US-0205: Wikipedia Article Capture
//
// Verifies: mock Wikipedia API, capture article,
// title/summary/infobox entities extracted.
// Both upstream fetches (summary + article) run through a single xrr
// cassette (testdata/cassettes/us0205_*), one recorded round-trip per
// path. In the default replay mode the fixture server is never contacted.
// See xrr_capture_helpers_test.go for what the cassettes do and do not
// prove.
//
// Re-record: XRR_MODE=record go test -count=1 ./test/integration/ \
//   -run TestUS0205_RecordCassettes
//
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
	// client carries the xrr record/replay transport. Nil falls back to
	// the default client.
	client *gohttp.Client
}

func (s *wikipediaFetchStep) Name() string { return "test-wikipedia-fetch" }
func (s *wikipediaFetchStep) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	// Fetch summary.
	summaryURL := fmt.Sprintf("%s/api/rest_v1/page/summary/%s", s.apiBaseURL, s.pageTitle)
	resp, err := captureGet(s.client, summaryURL)
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
	respArt, err := captureGet(s.client, articleURL)
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
// Fixture handlers
//
// The step makes two GETs per run (summary, then article), so each
// cassette directory holds two recorded round-trips — one per path.
// ---------------------------------------------------------------------------

const (
	wikiDistributedPage    = "Distributed_computing"
	wikiDistributedTitle   = "Distributed computing"
	wikiDistributedSummary = "Distributed computing is a field of computer science that studies distributed systems."
	wikiGoPage             = "Go_programming_language"
	wikiGoTitle            = "Go (programming language)"
	wikiGoSummary          = "Go is a statically typed compiled language."
)

func wikipediaSummaryPath(page string) string {
	return fmt.Sprintf("/api/rest_v1/page/summary/%s", page)
}

func wikipediaArticlePath(page string) string {
	return fmt.Sprintf("/api/rest_v1/page/article/%s", page)
}

// wikipediaHandler routes the REST v1 summary and article paths for one
// page, 404-ing anything else exactly as the previous inline handlers did.
func wikipediaHandler(page string, summary *mockWikipediaSummary, article *mockWikipediaArticle) gohttp.HandlerFunc {
	return func(w gohttp.ResponseWriter, r *gohttp.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case wikipediaSummaryPath(page):
			_ = json.NewEncoder(w).Encode(summary)
		case wikipediaArticlePath(page):
			_ = json.NewEncoder(w).Encode(article)
		default:
			w.WriteHeader(gohttp.StatusNotFound)
		}
	}
}

func wikipediaDistributedHandler() gohttp.HandlerFunc {
	return wikipediaHandler(wikiDistributedPage,
		&mockWikipediaSummary{
			Title:   wikiDistributedTitle,
			Extract: wikiDistributedSummary,
			PageID:  8071,
		},
		&mockWikipediaArticle{
			Title:   wikiDistributedTitle,
			Summary: wikiDistributedSummary,
			Infobox: []mockWikipediaInfoboxEntity{
				{Name: "discipline", Value: "Computer science"},
			},
		})
}

func wikipediaGoHandler() gohttp.HandlerFunc {
	return wikipediaHandler(wikiGoPage,
		&mockWikipediaSummary{
			Title:   wikiGoTitle,
			Extract: wikiGoSummary,
			PageID:  25039021,
		},
		&mockWikipediaArticle{
			Title:   wikiGoTitle,
			Summary: wikiGoSummary,
			Infobox: []mockWikipediaInfoboxEntity{
				{Name: "developer", Value: "Google"},
				{Name: "first_appeared", Value: "2009"},
				{Name: "paradigm", Value: "multi-paradigm"},
			},
		})
}

func wikipediaCassettes() []captureFixture {
	return []captureFixture{
		{
			Cassette: "us0205_title_summary",
			Handler:  wikipediaDistributedHandler(),
			Paths: []string{
				wikipediaSummaryPath(wikiDistributedPage),
				wikipediaArticlePath(wikiDistributedPage),
			},
		},
		{
			Cassette: "us0205_infobox_entities",
			Handler:  wikipediaGoHandler(),
			Paths: []string{
				wikipediaSummaryPath(wikiGoPage),
				wikipediaArticlePath(wikiGoPage),
			},
		},
	}
}

// TestUS0205_RecordCassettes re-records the US-0205 cassettes. No-op
// unless XRR_MODE=record; needs no Postgres/Redis.
func TestUS0205_RecordCassettes(t *testing.T) {
	recordCaptureFixtures(t, wikipediaCassettes())
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

	const pageTitle = wikiDistributedPage
	const wantTitle = wikiDistributedTitle
	const wantSummary = wikiDistributedSummary

	// Replay answers from the cassette; the fixture server only matters
	// when re-recording.
	srv := httptest.NewServer(wikipediaDistributedHandler())
	defer srv.Close()

	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Upsert("reference.wikipedia", &pipeline.Pipeline{
		PipelineName: "reference.wikipedia",
		Steps: []pipeline.PipelineStep{
			&wikipediaFetchStep{
				apiBaseURL: srv.URL,
				pageTitle:  pageTitle,
				client:     captureClient(t, "us0205_title_summary"),
			},
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

	const pageTitle = wikiGoPage

	srv := httptest.NewServer(wikipediaGoHandler())
	defer srv.Close()

	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Upsert("reference.wikipedia", &pipeline.Pipeline{
		PipelineName: "reference.wikipedia",
		Steps: []pipeline.PipelineStep{
			&wikipediaFetchStep{
				apiBaseURL: srv.URL,
				pageTitle:  pageTitle,
				client:     captureClient(t, "us0205_infobox_entities"),
			},
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

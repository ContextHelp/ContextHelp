package integration

// US-0203: arXiv Paper Capture and Indexing
//
// Verifies: mock arXiv API, capture paper by ID,
// title/abstract/authors/PDF URL stored on resulting object.
// All external HTTP calls use httptest.NewServer — no real network.
// Gate: INTEGRATION=1 env var required.

import (
	"context"
	"encoding/json"
	"encoding/xml"
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
// arXiv Atom feed types (subset used by tests)
// ---------------------------------------------------------------------------

type arxivEntry struct {
	XMLName xml.Name `xml:"entry"`
	ID      string   `xml:"id"`
	Title   string   `xml:"title"`
	Summary string   `xml:"summary"`
	Authors []struct {
		Name string `xml:"name"`
	} `xml:"author"`
	Links []struct {
		Href string `xml:"href,attr"`
		Type string `xml:"type,attr"`
	} `xml:"link"`
	Published string `xml:"published"`
}

type arxivFeed struct {
	XMLName xml.Name     `xml:"feed"`
	Entries []arxivEntry `xml:"entry"`
}

// ---------------------------------------------------------------------------
// Pipeline step
// ---------------------------------------------------------------------------

// arxivFetchStep calls the mock arXiv API endpoint and populates the draft.
type arxivFetchStep struct {
	pipeline.BaseContract
	apiBaseURL string
	arxivID    string
}

func (s *arxivFetchStep) Name() string { return "test-arxiv-fetch" }
func (s *arxivFetchStep) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	url := fmt.Sprintf("%s/query?id_list=%s", s.apiBaseURL, s.arxivID)
	resp, err := gohttp.Get(url)
	if err != nil {
		return draft, err
	}
	defer resp.Body.Close()

	var feed arxivFeed
	if err := xml.NewDecoder(resp.Body).Decode(&feed); err != nil {
		return draft, err
	}
	if len(feed.Entries) == 0 {
		return draft, fmt.Errorf("no entries returned for arxiv_id=%s", s.arxivID)
	}

	entry := feed.Entries[0]
	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}

	draft.Type = "academic.paper"
	draft.TextContent = entry.Summary

	authors := make([]string, len(entry.Authors))
	for i, a := range entry.Authors {
		authors[i] = a.Name
	}

	var pdfURL string
	for _, l := range entry.Links {
		if l.Type == "application/pdf" {
			pdfURL = l.Href
			break
		}
	}

	draft.Metadata["arxiv_id"] = s.arxivID
	draft.Metadata["title"] = entry.Title
	draft.Metadata["abstract"] = entry.Summary
	draft.Metadata["authors"] = authors
	draft.Metadata["pdf_url"] = pdfURL
	draft.Metadata["published"] = entry.Published
	return draft, nil
}

// ---------------------------------------------------------------------------
// US-0203 Tests
// ---------------------------------------------------------------------------

// TestUS0203_TitleAbstractAuthorsStored verifies title, abstract, authors, and
// PDF URL are stored on the captured KnowledgeObject.
func TestUS0203_TitleAbstractAuthorsStored(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("set INTEGRATION=1")
	}

	const arxivID = "2401.12345"
	const wantTitle = "Scaling Laws for Neural Language Models"
	const wantAbstract = "We study empirical scaling laws for language model performance."
	wantAuthors := []string{"Jared Kaplan", "Sam McCandlish"}

	srv := httptest.NewServer(gohttp.HandlerFunc(func(w gohttp.ResponseWriter, r *gohttp.Request) {
		w.Header().Set("Content-Type", "application/atom+xml")
		feed := arxivFeed{
			Entries: []arxivEntry{
				{
					ID:      fmt.Sprintf("http://arxiv.org/abs/%s", arxivID),
					Title:   wantTitle,
					Summary: wantAbstract,
					Authors: []struct {
						Name string `xml:"name"`
					}{
						{Name: "Jared Kaplan"},
						{Name: "Sam McCandlish"},
					},
					Links: []struct {
						Href string `xml:"href,attr"`
						Type string `xml:"type,attr"`
					}{
						{Href: fmt.Sprintf("https://arxiv.org/pdf/%s", arxivID), Type: "application/pdf"},
					},
					Published: "2026-01-15T00:00:00Z",
				},
			},
		}
		xml.NewEncoder(w).Encode(feed)
	}))
	defer srv.Close()

	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Upsert("academic.arxiv", &pipeline.Pipeline{
		PipelineName: "academic.arxiv",
		Steps: []pipeline.PipelineStep{
			&arxivFetchStep{apiBaseURL: srv.URL, arxivID: arxivID},
		},
	})

	sourceURL := fmt.Sprintf("https://arxiv.org/abs/%s", arxivID)
	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  sourceURL,
		Type:     "url",
		Pipeline: "academic.arxiv",
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

	assert.Equal(t, "academic.paper", obj.Type)
	assert.Equal(t, wantTitle, obj.Metadata["title"], "title must be stored")
	assert.Equal(t, wantAbstract, obj.Metadata["abstract"], "abstract must be stored")

	gotAuthors, _ := obj.Metadata["authors"].([]any)
	require.Len(t, gotAuthors, len(wantAuthors), "author count must match")
	for i, want := range wantAuthors {
		assert.Equal(t, want, gotAuthors[i], "author[%d] must match", i)
	}

	assert.Equal(t, fmt.Sprintf("https://arxiv.org/pdf/%s", arxivID), obj.Metadata["pdf_url"],
		"PDF URL must be stored")
}

// TestUS0203_ArxivIDStoredInMetadata verifies arxiv_id is stored for reference.
func TestUS0203_ArxivIDStoredInMetadata(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("set INTEGRATION=1")
	}

	const arxivID = "2404.99999"

	srv := httptest.NewServer(gohttp.HandlerFunc(func(w gohttp.ResponseWriter, r *gohttp.Request) {
		w.Header().Set("Content-Type", "application/atom+xml")
		feed := arxivFeed{
			Entries: []arxivEntry{
				{
					ID:      fmt.Sprintf("http://arxiv.org/abs/%s", arxivID),
					Title:   "Test Paper",
					Summary: "Test abstract.",
					Authors: []struct {
						Name string `xml:"name"`
					}{{Name: "Test Author"}},
					Published: "2026-04-01T00:00:00Z",
				},
			},
		}
		xml.NewEncoder(w).Encode(feed)
	}))
	defer srv.Close()

	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Upsert("academic.arxiv", &pipeline.Pipeline{
		PipelineName: "academic.arxiv",
		Steps: []pipeline.PipelineStep{
			&arxivFetchStep{apiBaseURL: srv.URL, arxivID: arxivID},
		},
	})

	sourceURL := fmt.Sprintf("https://arxiv.org/abs/%s", arxivID)
	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  sourceURL,
		Type:     "url",
		Pipeline: "academic.arxiv",
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

	assert.Equal(t, arxivID, obj.Metadata["arxiv_id"], "arxiv_id must be stored in metadata")
}

// TestUS0203_CaptureIsAsync verifies the arXiv capture returns job_id immediately.
func TestUS0203_CaptureIsAsync(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("set INTEGRATION=1")
	}

	srv := httptest.NewServer(gohttp.HandlerFunc(func(w gohttp.ResponseWriter, r *gohttp.Request) {
		w.Header().Set("Content-Type", "application/atom+xml")
		xml.NewEncoder(w).Encode(arxivFeed{
			Entries: []arxivEntry{
				{
					ID: "http://arxiv.org/abs/0000.00001", Title: "Async Test",
					Summary: "s", Published: "2026-01-01T00:00:00Z",
				},
			},
		})
	}))
	defer srv.Close()

	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Upsert("academic.arxiv", &pipeline.Pipeline{
		PipelineName: "academic.arxiv",
		Steps: []pipeline.PipelineStep{
			&arxivFetchStep{apiBaseURL: srv.URL, arxivID: "0000.00001"},
		},
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "https://arxiv.org/abs/0000.00001",
		Type:     "url",
		Pipeline: "academic.arxiv",
		Source:   "https://arxiv.org/abs/0000.00001",
	})
	require.NoError(t, err)
	assert.NotEmpty(t, jobID, "job_id must be returned immediately")
}

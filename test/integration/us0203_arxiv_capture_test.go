package integration

// US-0203: arXiv Paper Capture and Indexing
//
// Verifies: arXiv API fetch, capture paper by ID,
// title/abstract/authors/PDF URL stored on resulting object.
//
// The upstream fetch runs through an xrr cassette
// (testdata/cassettes/us0203_*). In the default replay mode the recorded
// round-trip answers from disk and the fixture server below is never
// contacted. See xrr_capture_helpers_test.go for what the cassettes do
// and do not prove.
//
// Re-record: INTEGRATION=1 XRR_MODE=record go test -count=1 \
//   ./test/integration/ -run TestUS0203
//
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
	// client carries the xrr record/replay transport. Nil falls back to
	// the default client, preserving the pre-cassette behavior.
	client *gohttp.Client
}

func (s *arxivFetchStep) Name() string { return "test-arxiv-fetch" }
func (s *arxivFetchStep) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	url := fmt.Sprintf("%s/query?id_list=%s", s.apiBaseURL, s.arxivID)
	resp, err := captureGet(s.client, url)
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
// Fixture handlers
//
// Each handler is named and shared between the test that replays its
// cassette and TestUS0203_RecordCassettes, which re-records it. Keeping
// one definition means a cassette can never silently disagree with the
// fixture it was recorded from.
// ---------------------------------------------------------------------------

const (
	arxivPaperID       = "2401.12345"
	arxivPaperTitle    = "Scaling Laws for Neural Language Models"
	arxivPaperAbstract = "We study empirical scaling laws for language model performance."
	arxivIDOnlyPaperID = "2404.99999"
	arxivAsyncPaperID  = "0000.00001"
)

var arxivPaperAuthors = []string{"Jared Kaplan", "Sam McCandlish"}

func arxivAtomHandler(feed arxivFeed) gohttp.HandlerFunc {
	return func(w gohttp.ResponseWriter, _ *gohttp.Request) {
		w.Header().Set("Content-Type", "application/atom+xml")
		_ = xml.NewEncoder(w).Encode(feed)
	}
}

// arxivPaperFeed is the full-metadata fixture: title, abstract, two
// authors and a PDF link.
func arxivPaperFeed() arxivFeed {
	return arxivFeed{
		Entries: []arxivEntry{
			{
				ID:      fmt.Sprintf("http://arxiv.org/abs/%s", arxivPaperID),
				Title:   arxivPaperTitle,
				Summary: arxivPaperAbstract,
				Authors: []struct {
					Name string `xml:"name"`
				}{
					{Name: arxivPaperAuthors[0]},
					{Name: arxivPaperAuthors[1]},
				},
				Links: []struct {
					Href string `xml:"href,attr"`
					Type string `xml:"type,attr"`
				}{
					{Href: fmt.Sprintf("https://arxiv.org/pdf/%s", arxivPaperID), Type: "application/pdf"},
				},
				Published: "2026-01-15T00:00:00Z",
			},
		},
	}
}

// arxivIDOnlyFeed is the minimal fixture used to assert arxiv_id lands in
// metadata: one author, no links.
func arxivIDOnlyFeed() arxivFeed {
	return arxivFeed{
		Entries: []arxivEntry{
			{
				ID:      fmt.Sprintf("http://arxiv.org/abs/%s", arxivIDOnlyPaperID),
				Title:   "Test Paper",
				Summary: "Test abstract.",
				Authors: []struct {
					Name string `xml:"name"`
				}{{Name: "Test Author"}},
				Published: "2026-04-01T00:00:00Z",
			},
		},
	}
}

// arxivAsyncFeed is the smallest viable entry — the async test only needs
// the fetch to succeed.
func arxivAsyncFeed() arxivFeed {
	return arxivFeed{
		Entries: []arxivEntry{
			{
				ID:        fmt.Sprintf("http://arxiv.org/abs/%s", arxivAsyncPaperID),
				Title:     "Async Test",
				Summary:   "s",
				Published: "2026-01-01T00:00:00Z",
			},
		},
	}
}

// arxivCassettes maps cassette name to the fixture and request that
// produced it. TestUS0203_RecordCassettes walks this table.
func arxivCassettes() []captureFixture {
	return []captureFixture{
		{
			Cassette: "us0203_paper_metadata",
			Handler:  arxivAtomHandler(arxivPaperFeed()),
			Path:     fmt.Sprintf("/query?id_list=%s", arxivPaperID),
		},
		{
			Cassette: "us0203_arxiv_id_metadata",
			Handler:  arxivAtomHandler(arxivIDOnlyFeed()),
			Path:     fmt.Sprintf("/query?id_list=%s", arxivIDOnlyPaperID),
		},
		{
			Cassette: "us0203_async_capture",
			Handler:  arxivAtomHandler(arxivAsyncFeed()),
			Path:     fmt.Sprintf("/query?id_list=%s", arxivAsyncPaperID),
		},
	}
}

// TestUS0203_RecordCassettes re-records the US-0203 cassettes from the
// fixture handlers above. It is a no-op unless XRR_MODE=record, and it
// needs no Postgres/Redis because it drives only the HTTP seam.
func TestUS0203_RecordCassettes(t *testing.T) {
	recordCaptureFixtures(t, arxivCassettes())
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

	const arxivID = arxivPaperID
	const wantTitle = arxivPaperTitle
	const wantAbstract = arxivPaperAbstract
	wantAuthors := arxivPaperAuthors

	// Replay answers from the cassette; the fixture server only matters
	// when re-recording.
	srv := httptest.NewServer(arxivAtomHandler(arxivPaperFeed()))
	defer srv.Close()

	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Upsert("academic.arxiv", &pipeline.Pipeline{
		PipelineName: "academic.arxiv",
		Steps: []pipeline.PipelineStep{
			&arxivFetchStep{
				apiBaseURL: srv.URL,
				arxivID:    arxivID,
				client:     captureClient(t, "us0203_paper_metadata"),
			},
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

	const arxivID = arxivIDOnlyPaperID

	srv := httptest.NewServer(arxivAtomHandler(arxivIDOnlyFeed()))
	defer srv.Close()

	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Upsert("academic.arxiv", &pipeline.Pipeline{
		PipelineName: "academic.arxiv",
		Steps: []pipeline.PipelineStep{
			&arxivFetchStep{
				apiBaseURL: srv.URL,
				arxivID:    arxivID,
				client:     captureClient(t, "us0203_arxiv_id_metadata"),
			},
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

	srv := httptest.NewServer(arxivAtomHandler(arxivAsyncFeed()))
	defer srv.Close()

	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Upsert("academic.arxiv", &pipeline.Pipeline{
		PipelineName: "academic.arxiv",
		Steps: []pipeline.PipelineStep{
			&arxivFetchStep{
				apiBaseURL: srv.URL,
				arxivID:    arxivAsyncPaperID,
				client:     captureClient(t, "us0203_async_capture"),
			},
		},
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "https://arxiv.org/abs/" + arxivAsyncPaperID,
		Type:     "url",
		Pipeline: "academic.arxiv",
		Source:   "https://arxiv.org/abs/" + arxivAsyncPaperID,
	})
	require.NoError(t, err)
	assert.NotEmpty(t, jobID, "job_id must be returned immediately")
}

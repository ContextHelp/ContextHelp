package integration

// US-0002: URL capture.
//
// Drives the real url.generic pipeline (url_fetcher, html_cleaner, ...)
// against an httptest page server. Nothing but the remote site is faked.

import (
	"fmt"
	gohttp "net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

const (
	us0002Title = "Understanding Distributed Systems"
	us0002Body  = "Distributed systems require careful coordination between nodes."
)

// startPageServer serves one article and counts requests.
func startPageServer(t *testing.T) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	var hits atomic.Int64
	srv := httptest.NewServer(gohttp.HandlerFunc(func(w gohttp.ResponseWriter, r *gohttp.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<html><head><title>%s</title><script>var tracker=1;</script></head>`+
			`<body><article><h1>Heading</h1><p>%s</p></article></body></html>`, us0002Title, us0002Body)
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

func ingestURL(t *testing.T, env *testEnv, url string) (*storage.Job, storage.KnowledgeObject) {
	t.Helper()
	jobID, err := env.svc.Analyze(ownerCtx(), service.AnalyzeRequest{
		Content: url, Type: "url", Source: url,
	})
	require.NoError(t, err)
	job := waitForJob(t, env.URL, jobID, storage.JobCompleted)
	require.NotEmpty(t, job.ResultID)
	return job, getObject(t, env.URL, job.ResultID)
}

// POST /analyze with a URL returns 202 + job_id and the job is selected onto
// the url.generic pipeline without the caller naming one.
func TestUS0002_URLIngestReturnsJobID(t *testing.T) {
	srv, _ := startPageServer(t)
	env := startTestEnv(t)
	defer env.stop(t)

	jobID := postForJobID(t, env.URL+"/api/v1/analyze", []byte(fmt.Sprintf(
		`{"content":%q,"type":"url","source":%q}`, srv.URL+"/a", srv.URL+"/a")))
	require.NotEmpty(t, jobID)

	job := waitForJob(t, env.URL, jobID, storage.JobCompleted)
	assert.Equal(t, "url.generic", job.Pipeline)
}

// The fetched page's title and body text land in the stored object and
// script content does not.
func TestUS0002_TitleAndBodyExtracted(t *testing.T) {
	srv, hits := startPageServer(t)
	env := startTestEnv(t)
	defer env.stop(t)

	_, obj := ingestURL(t, env, srv.URL+"/a")

	assert.EqualValues(t, 1, hits.Load(), "page must be fetched exactly once")
	assert.Contains(t, obj.TextContent, us0002Title)
	assert.Contains(t, obj.TextContent, us0002Body)
	assert.NotContains(t, obj.TextContent, "tracker", "script content must be stripped")
	assert.Contains(t, obj.Metadata["content_type"], "text/html")
}

// The original URL is stored as the object's source.
func TestUS0002_SourceURLStoredOnObject(t *testing.T) {
	srv, _ := startPageServer(t)
	env := startTestEnv(t)
	defer env.stop(t)

	url := srv.URL + "/source-url-test"
	_, obj := ingestURL(t, env, url)

	assert.Equal(t, url, obj.Source)
	assert.Equal(t, url, obj.Metadata["final_url"])
}

// Re-ingesting an unchanged URL resolves to the same object (content-hash
// dedup) instead of creating a duplicate.
func TestUS0002_DedupOnReIngestSameURL(t *testing.T) {
	srv, _ := startPageServer(t)
	env := startTestEnv(t)
	defer env.stop(t)

	url := srv.URL + "/dedup-test"
	job1, obj1 := ingestURL(t, env, url)
	job2, obj2 := ingestURL(t, env, url)

	assert.NotEqual(t, job1.ID, job2.ID)
	assert.Equal(t, obj1.ID, obj2.ID, "unchanged page must not create a second object")
	assert.Equal(t, obj1.ContentHash, obj2.ContentHash)
}

// The pipeline that produced the object is recorded on it.
func TestUS0002_URLPipelineAssigned(t *testing.T) {
	srv, _ := startPageServer(t)
	env := startTestEnv(t)
	defer env.stop(t)

	_, obj := ingestURL(t, env, srv.URL+"/pipeline-test")
	assert.Equal(t, "url.generic", obj.Pipeline)
}

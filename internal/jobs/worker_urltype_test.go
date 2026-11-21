package jobs

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline/steps"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

func TestJobObjectType(t *testing.T) {
	cases := map[string]string{
		"ingest:url":               "url",
		"ingest:url:nofanout":      "url",
		"ingest:text":              "",
		"ingest:text:nofanout":     "",
		"ingest:auto":              "",
		"ingest:":                  "",
		"ingest":                   "",
		"":                         "",
		"ingest:feed_item":         "",
		"ingest:github_release":    "",
		"ingest:image":             "image",
		"ingest:document:nofanout": "document",
		"other:url":                "",
	}
	for in, want := range cases {
		if got := jobObjectType(in); got != want {
			t.Errorf("jobObjectType(%q) = %q, want %q", in, got, want)
		}
	}
}

// runIngest runs one job through a pipeline of the real fetcher and
// type detector and returns the stored object.
func runIngest(t *testing.T, jobType, payload string, srv *httptest.Server) *storage.KnowledgeObject {
	t.Helper()
	driver := storageutil.NewTestDriver(t)
	q := NewQueue(driver.Jobs())
	pipes := pipeline.NewRegistry()
	pipes.Upsert("url.test", &pipeline.Pipeline{
		PipelineName: "url.test",
		Steps: []pipeline.PipelineStep{
			steps.NewURLFetcher(steps.WithURLHTTPClient(srv.Client())),
			steps.NewTypeDetector(),
		},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	job := makeJob("job-url")
	job.Type = jobType
	job.Payload = payload
	job.Source = payload
	job.Pipeline = "url.test"
	if err := q.Enqueue(ctx, job); err != nil {
		t.Fatal(err)
	}
	pool := NewWorkerPool(q, pipes, driver, 1, nil, defaultTestJobsCfg())
	go func() {
		for {
			got, _ := q.Get(ctx, "job-url")
			if got != nil && (got.Status == storage.JobCompleted || got.Status == storage.JobFailed) {
				cancel()
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
	}()
	pool.Start(ctx)

	got, _ := q.Get(context.Background(), "job-url")
	if got.Status != storage.JobCompleted {
		t.Fatalf("status = %q, want completed", got.Status)
	}
	obj, err := driver.Objects().Get(context.Background(), got.ResultID)
	if err != nil {
		t.Fatal(err)
	}
	return obj
}

func TestIngestURLJobStoresTypeURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("a fetched page body that is plain prose"))
	}))
	defer srv.Close()

	for _, jt := range []string{"ingest:url", "ingest:url:nofanout"} {
		obj := runIngest(t, jt, srv.URL+"/page", srv)
		if obj.Type != "url" {
			t.Errorf("%s: Type = %q, want url", jt, obj.Type)
		}
		if obj.Subtype != "short" {
			t.Errorf("%s: Subtype = %q, want short (content kind stays in subtype)", jt, obj.Subtype)
		}
	}
}

func TestIngestTextJobStillText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("body"))
	}))
	defer srv.Close()
	// Fetcher replaces the payload with the body, so the detector sees
	// plain text and a text job must stay text.
	if obj := runIngest(t, "ingest:text", srv.URL+"/p", srv); obj.Type != "text" {
		t.Errorf("Type = %q, want text", obj.Type)
	}
}

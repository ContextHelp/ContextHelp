package jobs

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// releaseJSON returns a minimal GitHub latest-release JSON payload.
func releaseJSON(tag, htmlURL string, published time.Time) []byte {
	data, _ := json.Marshal(map[string]any{
		"tag_name":     tag,
		"html_url":     htmlURL,
		"published_at": published.Format(time.RFC3339),
	})
	return data
}

// mockReleaseServer returns a test server responding with the given tag/URL.
// statusCode 404 simulates "no releases".
func mockReleaseServer(t *testing.T, tag, htmlURL string, statusCode int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if statusCode != http.StatusOK {
			w.WriteHeader(statusCode)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(releaseJSON(tag, htmlURL, time.Now()))
	}))
}

// interceptRT rewrites every outgoing request host to the test server address.
type interceptRT struct {
	addr    string
	wrapped http.RoundTripper
}

func (rt *interceptRT) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.URL.Scheme = "http"
	clone.URL.Host = rt.addr
	return rt.wrapped.RoundTrip(clone)
}

// newTestWatcher builds a ReleaseWatcher wired to srv with an in-memory queue.
func newTestWatcher(t *testing.T, srv *httptest.Server, repos []string, stateFile string) *ReleaseWatcher {
	t.Helper()
	driver := storageutil.NewTestDriver(t)
	q := NewQueue(driver.Jobs())
	client := &http.Client{
		Transport: &interceptRT{
			addr:    srv.Listener.Addr().String(),
			wrapped: http.DefaultTransport,
		},
	}
	return &ReleaseWatcher{
		Repos:      repos,
		StateFile:  stateFile,
		Queue:      q,
		MaxRetries: 1,
		httpClient: client,
	}
}

// allJobs lists all jobs regardless of status.
func allJobs(t *testing.T, w *ReleaseWatcher) []*storage.Job {
	t.Helper()
	jobs, _, err := w.Queue.List(context.Background(), storage.JobFilter{})
	if err != nil {
		t.Fatalf("list jobs: %v", err)
	}
	return jobs
}

// TestNoNewRelease: last-seen tag matches API — no job enqueued.
func TestNoNewRelease(t *testing.T) {
	tag := "v1.0.0"
	htmlURL := "https://github.com/acme/foo/releases/tag/v1.0.0"

	srv := mockReleaseServer(t, tag, htmlURL, http.StatusOK)
	defer srv.Close()

	stateFile := filepath.Join(t.TempDir(), "state.json")
	initial := watcherState{LastSeen: map[string]string{"acme/foo": tag}}
	data, _ := json.Marshal(initial)
	os.WriteFile(stateFile, data, 0o600)

	w := newTestWatcher(t, srv, []string{"acme/foo"}, stateFile)
	if err := w.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if got := allJobs(t, w); len(got) != 0 {
		t.Errorf("expected 0 jobs, got %d", len(got))
	}
}

// TestNewReleaseEnqueued: new tag → one pipeline job with correct URL.
func TestNewReleaseEnqueued(t *testing.T) {
	tag := "v2.0.0"
	htmlURL := "https://github.com/acme/bar/releases/tag/v2.0.0"

	srv := mockReleaseServer(t, tag, htmlURL, http.StatusOK)
	defer srv.Close()

	stateFile := filepath.Join(t.TempDir(), "state.json")
	w := newTestWatcher(t, srv, []string{"acme/bar"}, stateFile)

	if err := w.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	jobs := allJobs(t, w)
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}
	job := jobs[0]
	if job.Type != "ingest:github_release" {
		t.Errorf("type: got %q", job.Type)
	}
	if job.Payload != htmlURL {
		t.Errorf("payload: got %q, want %q", job.Payload, htmlURL)
	}
	if job.Pipeline != "github.release" {
		t.Errorf("pipeline: got %q", job.Pipeline)
	}
}

// TestStatePersistenceNoDuplicateEnqueue: second Run must not re-enqueue same release.
func TestStatePersistenceNoDuplicateEnqueue(t *testing.T) {
	tag := "v3.0.0"
	htmlURL := "https://github.com/acme/baz/releases/tag/v3.0.0"

	srv := mockReleaseServer(t, tag, htmlURL, http.StatusOK)
	defer srv.Close()

	stateFile := filepath.Join(t.TempDir(), "state.json")
	w := newTestWatcher(t, srv, []string{"acme/baz"}, stateFile)

	// First run: enqueues.
	if err := w.Run(context.Background()); err != nil {
		t.Fatalf("first Run: %v", err)
	}
	if got := allJobs(t, w); len(got) != 1 {
		t.Fatalf("after first run: expected 1 job, got %d", len(got))
	}

	// Second run on same watcher (state persisted to file): must not enqueue again.
	if err := w.Run(context.Background()); err != nil {
		t.Fatalf("second Run: %v", err)
	}
	if got := allJobs(t, w); len(got) != 1 {
		t.Errorf("after second run: expected still 1 job, got %d", len(got))
	}
}

// TestNoReleasesNotFound: 404 from GitHub API → no error, no job.
func TestNoReleasesNotFound(t *testing.T) {
	srv := mockReleaseServer(t, "", "", http.StatusNotFound)
	defer srv.Close()

	stateFile := filepath.Join(t.TempDir(), "state.json")
	w := newTestWatcher(t, srv, []string{"acme/empty"}, stateFile)

	if err := w.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := allJobs(t, w); len(got) != 0 {
		t.Errorf("expected 0 jobs, got %d", len(got))
	}
}

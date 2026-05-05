package http

import (
	"net/http"
	"testing"
)

// TestArchivePipeline_MissingReturns404 confirms the handler maps the
// service's "pipeline %q not found" error to HTTP 404 instead of 500.
// Regression guard for the contract change introduced when T-1294
// added a Get-before-Update inside service.ArchivePipeline (the prior
// storage path silently no-op'd on missing rows).
func TestArchivePipeline_MissingReturns404(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()

	req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/pipelines/does-not-exist/archive", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set(HeaderCtxtNote, "ops review")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status: got %d, want 404", resp.StatusCode)
	}
}

// TestUnarchivePipeline_MissingReturns404 mirrors the archive guard
// for the unarchive path. Same root cause (T-1294 Get-before-Update),
// same contract preservation.
func TestUnarchivePipeline_MissingReturns404(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()

	req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/pipelines/does-not-exist/unarchive", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status: got %d, want 404", resp.StatusCode)
	}
}

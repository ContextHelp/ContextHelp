package http

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
)

func TestRunDropboxImport(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()

	body := bytes.NewBufferString(`{
		"access_token": "dbx-test-token",
		"path": "/documents",
		"profile": "personal"
	}`)
	resp, err := http.Post(ts.URL+"/api/v1/importers/dropbox/run", "application/json", body)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		t.Errorf("status: got %d, want 202", resp.StatusCode)
	}

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if result["run_id"] == "" {
		t.Error("expected non-empty run_id")
	}
	if result["status"] != "pending" {
		t.Errorf("status: got %v, want pending", result["status"])
	}
	if result["pipeline"] != "dropbox.sync" {
		t.Errorf("pipeline: got %v", result["pipeline"])
	}
}

func TestRunDropboxImportDryRun(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()

	body := bytes.NewBufferString(`{
		"access_token": "dbx-token",
		"dry_run": true
	}`)
	resp, err := http.Post(ts.URL+"/api/v1/importers/dropbox/run", "application/json", body)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status: got %d, want 200", resp.StatusCode)
	}

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if result["dry_run"] != true {
		t.Errorf("dry_run: got %v, want true", result["dry_run"])
	}
	if result["status"] != "dry_run" {
		t.Errorf("status: got %v, want dry_run", result["status"])
	}
}

func TestRunDropboxImportMissingToken(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()

	body := bytes.NewBufferString(`{"path": "/documents"}`)
	resp, err := http.Post(ts.URL+"/api/v1/importers/dropbox/run", "application/json", body)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status: got %d, want 400", resp.StatusCode)
	}
}

func TestRunDropboxImportInvalidJSON(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()

	body := bytes.NewBufferString(`{invalid json}`)
	resp, err := http.Post(ts.URL+"/api/v1/importers/dropbox/run", "application/json", body)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status: got %d, want 400", resp.StatusCode)
	}
}

func TestGetImporterRun(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()

	// First, create a run.
	createBody := bytes.NewBufferString(`{"access_token": "tok"}`)
	createResp, err := http.Post(ts.URL+"/api/v1/importers/dropbox/run", "application/json", createBody)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	defer createResp.Body.Close()

	var created map[string]any
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	runID, _ := created["run_id"].(string)
	if runID == "" {
		t.Fatal("expected run_id in create response")
	}

	// Now retrieve the run.
	resp, err := http.Get(ts.URL + "/api/v1/importers/runs/" + runID)
	if err != nil {
		t.Fatalf("get request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status: got %d, want 200", resp.StatusCode)
	}

	var run map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&run); err != nil {
		t.Fatalf("decode run response: %v", err)
	}
	if run["run_id"] != runID {
		t.Errorf("run_id: got %v, want %v", run["run_id"], runID)
	}
}

func TestGetImporterRunNotFound(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v1/importers/runs/nonexistent-id")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status: got %d, want 404", resp.StatusCode)
	}
}

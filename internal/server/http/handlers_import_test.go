package http

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestCreateImport(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()

	body := bytes.NewBufferString(`{"content":"{\"content\":\"hello\"}","format":"jsonl"}`)
	resp, err := http.Post(ts.URL+"/api/v1/import", "application/json", body)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		t.Errorf("status: got %d, want 202", resp.StatusCode)
	}

	var batch storage.Batch
	json.NewDecoder(resp.Body).Decode(&batch)
	if batch.ID == "" {
		t.Error("expected non-empty batch ID")
	}
	if batch.Format != "jsonl" {
		t.Errorf("Format: got %q, want jsonl", batch.Format)
	}
}

func TestCreateImportMissingContent(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()

	body := bytes.NewBufferString(`{"format":"jsonl"}`)
	resp, err := http.Post(ts.URL+"/api/v1/import", "application/json", body)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status: got %d, want 400", resp.StatusCode)
	}
}

func TestCreateImportDefaultFormat(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()

	body := bytes.NewBufferString(`{"content":"some content"}`)
	resp, err := http.Post(ts.URL+"/api/v1/import", "application/json", body)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		t.Errorf("status: got %d, want 202", resp.StatusCode)
	}

	var batch storage.Batch
	json.NewDecoder(resp.Body).Decode(&batch)
	if batch.Format != "jsonl" {
		t.Errorf("Format: got %q, want jsonl (default)", batch.Format)
	}
}

func TestGetImport(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()

	// Create a batch first.
	body := bytes.NewBufferString(`{"content":"data","format":"csv"}`)
	createResp, err := http.Post(ts.URL+"/api/v1/import", "application/json", body)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	defer createResp.Body.Close()

	var created storage.Batch
	json.NewDecoder(createResp.Body).Decode(&created)

	// Retrieve it.
	resp, err := http.Get(ts.URL + "/api/v1/import/" + created.ID)
	if err != nil {
		t.Fatalf("get request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status: got %d, want 200", resp.StatusCode)
	}

	var batch storage.Batch
	json.NewDecoder(resp.Body).Decode(&batch)
	if batch.ID != created.ID {
		t.Errorf("ID: got %q, want %q", batch.ID, created.ID)
	}
}

func TestGetImportNotFound(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v1/import/nonexistent")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status: got %d, want 404", resp.StatusCode)
	}
}

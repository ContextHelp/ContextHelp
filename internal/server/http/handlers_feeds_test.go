package http

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestCreateFeed(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()

	body := bytes.NewBufferString(`{"url":"https://example.com/feed.xml"}`)
	resp, err := http.Post(ts.URL+"/api/v1/feeds", "application/json", body)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Errorf("status: got %d, want 201", resp.StatusCode)
	}

	var feed storage.Feed
	json.NewDecoder(resp.Body).Decode(&feed)
	if feed.ID == "" {
		t.Error("expected non-empty feed ID")
	}
	if feed.URL != "https://example.com/feed.xml" {
		t.Errorf("URL: got %q", feed.URL)
	}
	if feed.Status != "active" {
		t.Errorf("Status: got %q, want active", feed.Status)
	}
}

func TestCreateFeedMissingURL(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()

	body := bytes.NewBufferString(`{}`)
	resp, err := http.Post(ts.URL+"/api/v1/feeds", "application/json", body)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status: got %d, want 400", resp.StatusCode)
	}
}

func TestCreateFeedInvalidURL(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()

	body := bytes.NewBufferString(`{"url":"not-a-url"}`)
	resp, err := http.Post(ts.URL+"/api/v1/feeds", "application/json", body)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status: got %d, want 400", resp.StatusCode)
	}
}

func TestListFeeds(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()

	// Create two feeds.
	for _, u := range []string{"https://a.com/feed", "https://b.com/feed"} {
		body := bytes.NewBufferString(`{"url":"` + u + `"}`)
		http.Post(ts.URL+"/api/v1/feeds", "application/json", body)
	}

	resp, err := http.Get(ts.URL + "/api/v1/feeds")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status: got %d, want 200", resp.StatusCode)
	}

	var body struct {
		Feeds []*storage.Feed `json:"feeds"`
		Total int             `json:"total"`
	}
	json.NewDecoder(resp.Body).Decode(&body)
	if len(body.Feeds) != 2 {
		t.Errorf("expected 2 feeds, got %d", len(body.Feeds))
	}
}

func TestDeleteFeed(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()

	// Create a feed.
	body := bytes.NewBufferString(`{"url":"https://example.com/feed.xml"}`)
	resp, err := http.Post(ts.URL+"/api/v1/feeds", "application/json", body)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	defer resp.Body.Close()

	var feed storage.Feed
	json.NewDecoder(resp.Body).Decode(&feed)

	// Delete it.
	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/v1/feeds/"+feed.ID, nil)
	delResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("delete request: %v", err)
	}
	defer delResp.Body.Close()

	if delResp.StatusCode != http.StatusNoContent {
		t.Errorf("status: got %d, want 204", delResp.StatusCode)
	}
}

func TestSyncFeed(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()

	// Create a feed.
	body := bytes.NewBufferString(`{"url":"https://example.com/feed.xml"}`)
	resp, _ := http.Post(ts.URL+"/api/v1/feeds", "application/json", body)
	defer resp.Body.Close()

	var feed storage.Feed
	json.NewDecoder(resp.Body).Decode(&feed)

	// Trigger sync.
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/feeds/"+feed.ID+"/sync", nil)
	syncResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("sync request: %v", err)
	}
	defer syncResp.Body.Close()

	if syncResp.StatusCode != http.StatusAccepted {
		t.Errorf("status: got %d, want 202", syncResp.StatusCode)
	}

	var result map[string]string
	json.NewDecoder(syncResp.Body).Decode(&result)
	if result["job_id"] == "" {
		t.Error("expected non-empty job_id")
	}
}

func TestSyncFeedNotFound(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/feeds/nonexistent/sync", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status: got %d, want 404", resp.StatusCode)
	}
}

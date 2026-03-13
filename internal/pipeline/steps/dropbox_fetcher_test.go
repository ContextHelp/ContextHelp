package steps

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	dropboximporter "github.com/ideacrafterslabs/ctxt/internal/importer/dropbox"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func dropboxTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/2/files/list_folder" {
			type entry struct {
				Tag            string `json:".tag"`
				ID             string `json:"id"`
				Name           string `json:"name"`
				PathDisplay    string `json:"path_display"`
				Size           int64  `json:"size"`
				ServerModified string `json:"server_modified"`
				ContentHash    string `json:"content_hash"`
			}
			type resp struct {
				Entries []entry `json:"entries"`
				Cursor  string  `json:"cursor"`
				HasMore bool    `json:"has_more"`
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp{
				Entries: []entry{
					{Tag: "file", ID: "id:f1", Name: "doc.md", PathDisplay: "/doc.md",
						Size: 200, ServerModified: time.Now().UTC().Format("2006-01-02T15:04:05Z"),
						ContentHash: "abc123"},
					{Tag: "folder", ID: "id:d1", Name: "archive", PathDisplay: "/archive"},
				},
				Cursor:  "cursor-xyz",
				HasMore: false,
			})
			return
		}
		http.NotFound(w, r)
	}))
}

func TestDropboxFetcherSuccess(t *testing.T) {
	srv := dropboxTestServer(t)
	defer srv.Close()

	cl := dropboximporter.NewClient(srv.Client(), srv.URL, srv.URL, "test-token")
	step := NewDropboxFetcher(WithDropboxClient(cl))

	draft := &storage.KnowledgeObject{
		Metadata: map[string]any{
			"dropbox_token": "test-token",
			"dropbox_path":  "",
		},
	}

	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	files, ok := got.Metadata["dropbox_files"].([]map[string]any)
	if !ok {
		t.Fatalf("dropbox_files has unexpected type %T", got.Metadata["dropbox_files"])
	}
	if len(files) != 2 {
		t.Fatalf("expected 2 file records, got %d", len(files))
	}
	if files[0]["name"] != "doc.md" {
		t.Errorf("file[0].name: got %v", files[0]["name"])
	}
	if files[1]["is_folder"] != true {
		t.Errorf("file[1].is_folder: want true")
	}

	cursor, _ := got.Metadata["dropbox_cursor"].(string)
	if cursor != "cursor-xyz" {
		t.Errorf("dropbox_cursor: got %q, want cursor-xyz", cursor)
	}
}

func TestDropboxFetcherMissingToken(t *testing.T) {
	step := NewDropboxFetcher()
	draft := &storage.KnowledgeObject{
		Metadata: map[string]any{},
	}
	_, err := step.Run(context.Background(), draft)
	if err == nil {
		t.Fatal("expected error for missing token")
	}
}

func TestDropboxFetcherNilMetadata(t *testing.T) {
	step := NewDropboxFetcher()
	draft := &storage.KnowledgeObject{}
	_, err := step.Run(context.Background(), draft)
	if err == nil {
		t.Fatal("expected error for nil metadata / missing token")
	}
}

func TestDropboxFetcherName(t *testing.T) {
	step := NewDropboxFetcher()
	if step.Name() != "dropbox_fetcher" {
		t.Errorf("Name: got %q", step.Name())
	}
}

func TestDropboxFetcherCursorPassthrough(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/2/files/list_folder/continue" {
			type resp struct {
				Entries []struct{} `json:"entries"`
				Cursor  string     `json:"cursor"`
				HasMore bool       `json:"has_more"`
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp{Cursor: "cursor-2", HasMore: false})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	cl := dropboximporter.NewClient(srv.Client(), srv.URL, srv.URL, "tok")
	step := NewDropboxFetcher(WithDropboxClient(cl))

	draft := &storage.KnowledgeObject{
		Metadata: map[string]any{
			"dropbox_token":  "tok",
			"dropbox_cursor": "saved-cursor",
		},
	}

	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("Run with cursor: %v", err)
	}

	cursor, _ := got.Metadata["dropbox_cursor"].(string)
	if cursor != "cursor-2" {
		t.Errorf("dropbox_cursor: got %q, want cursor-2", cursor)
	}
}

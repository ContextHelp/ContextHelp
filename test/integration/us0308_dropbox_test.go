package integration

// US-0308: Dropbox import via mocked API.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/importer/dropbox"
)

type dropboxListFolderResult struct {
	Entries []dropboxEntry `json:"entries"`
	Cursor  string         `json:"cursor"`
	HasMore bool           `json:"has_more"`
}

type dropboxEntry struct {
	Tag            string `json:".tag"`
	ID             string `json:"id"`
	Name           string `json:"name"`
	PathDisplay    string `json:"path_display"`
	Size           int64  `json:"size"`
	ServerModified string `json:"server_modified"`
	ContentHash    string `json:"content_hash"`
}

func makeDropboxServer(t *testing.T, entries []dropboxEntry) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/2/files/list_folder":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(dropboxListFolderResult{
				Entries: entries,
				Cursor:  "test-cursor",
				HasMore: false,
			})
		default:
			http.NotFound(w, r)
		}
	}))
}

// TestUS0308_DropboxListFiles verifies file list from mocked Dropbox API.
func TestUS0308_DropboxListFiles(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC().Truncate(time.Second)
	entries := []dropboxEntry{
		{Tag: "file", ID: "id:f1", Name: "notes.md", PathDisplay: "/notes.md",
			Size: 1024, ServerModified: now.Format("2006-01-02T15:04:05Z"), ContentHash: "hash1"},
		{Tag: "file", ID: "id:f2", Name: "draft.txt", PathDisplay: "/drafts/draft.txt",
			Size: 512, ServerModified: now.Format("2006-01-02T15:04:05Z"), ContentHash: "hash2"},
		{Tag: "folder", ID: "id:d1", Name: "drafts", PathDisplay: "/drafts"},
	}

	srv := makeDropboxServer(t, entries)
	defer srv.Close()

	client := dropbox.NewClient(nil, srv.URL, srv.URL, "test-token")
	result, err := client.ListFiles(context.Background(), dropbox.ListOptions{})
	require.NoError(t, err)

	files := result.Files
	require.NotEmpty(t, files, "must return at least one file")

	// Find the notes.md file entry.
	var notesFile *dropbox.File
	for i := range files {
		if files[i].Name == "notes.md" {
			notesFile = &files[i]
			break
		}
	}
	require.NotNil(t, notesFile, "notes.md must be in result")
	assert.Equal(t, "/notes.md", notesFile.Path)
	assert.False(t, notesFile.IsFolder, "notes.md must not be a folder")
}

// TestUS0308_DropboxRequiresToken verifies token validation.
func TestUS0308_DropboxRequiresToken(t *testing.T) {
	t.Parallel()

	srv := makeDropboxServer(t, nil)
	defer srv.Close()

	client := dropbox.NewClient(nil, srv.URL, srv.URL, "")
	_, err := client.ListFiles(context.Background(), dropbox.ListOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "access token")
}

// TestUS0308_DropboxCursorReturned verifies cursor is returned for incremental sync.
func TestUS0308_DropboxCursorReturned(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	entries := []dropboxEntry{
		{Tag: "file", ID: "id:f1", Name: "a.md", PathDisplay: "/a.md",
			ServerModified: now, ContentHash: "h1"},
	}

	srv := makeDropboxServer(t, entries)
	defer srv.Close()

	client := dropbox.NewClient(nil, srv.URL, srv.URL, "tok")
	result, err := client.ListFiles(context.Background(), dropbox.ListOptions{})
	require.NoError(t, err)
	assert.NotEmpty(t, result.Cursor, "cursor must be returned for incremental sync")
}

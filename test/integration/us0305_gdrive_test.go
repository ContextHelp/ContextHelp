package integration

// US-0305: Google Drive import via mocked API.
//
// Uses httptest.NewServer to mock the Drive v3 API.
// No real credentials required.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/importer/gdrive"
)

func makeGDriveServer(t *testing.T, files []map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/drive/v3/files" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") == "" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"files":         files,
			"nextPageToken": "",
		})
	}))
}

// TestUS0305_GDriveListFiles verifies the Drive client returns File records
// from the mocked API response.
func TestUS0305_GDriveListFiles(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC().Truncate(time.Second)
	srv := makeGDriveServer(t, []map[string]any{
		{
			"id":             "file-001",
			"name":           "Design Notes.gdoc",
			"mimeType":       "application/vnd.google-apps.document",
			"modifiedTime":   now.Format(time.RFC3339),
			"webViewLink":    "https://docs.google.com/document/d/file-001/edit",
			"webContentLink": "",
		},
		{
			"id":             "file-002",
			"name":           "Architecture.pdf",
			"mimeType":       "application/pdf",
			"modifiedTime":   now.Format(time.RFC3339),
			"webViewLink":    "https://drive.google.com/file/d/file-002/view",
			"webContentLink": "https://drive.google.com/uc?id=file-002",
		},
	})
	defer srv.Close()

	client := gdrive.NewClient(nil, srv.URL, "test-token")
	files, err := client.ListFiles(context.Background(), gdrive.ListOptions{})
	require.NoError(t, err)
	require.Len(t, files, 2)

	assert.Equal(t, "file-001", files[0].ID)
	assert.Equal(t, "Design Notes.gdoc", files[0].Name)
	assert.Equal(t, "application/vnd.google-apps.document", files[0].MimeType)
}

// TestUS0305_GDriveListFilesRequiresToken verifies token validation.
func TestUS0305_GDriveListFilesRequiresToken(t *testing.T) {
	t.Parallel()

	srv := makeGDriveServer(t, nil)
	defer srv.Close()

	client := gdrive.NewClient(nil, srv.URL, "") // empty token
	_, err := client.ListFiles(context.Background(), gdrive.ListOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "token")
}

// TestUS0305_GDriveMaxItemsRespected verifies MaxItems cap is respected.
func TestUS0305_GDriveMaxItemsRespected(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC().Format(time.RFC3339)
	apiFiles := []map[string]any{
		{"id": "f1", "name": "A.txt", "mimeType": "text/plain", "modifiedTime": now},
		{"id": "f2", "name": "B.txt", "mimeType": "text/plain", "modifiedTime": now},
		{"id": "f3", "name": "C.txt", "mimeType": "text/plain", "modifiedTime": now},
	}
	srv := makeGDriveServer(t, apiFiles)
	defer srv.Close()

	client := gdrive.NewClient(nil, srv.URL, "tok")
	files, err := client.ListFiles(context.Background(), gdrive.ListOptions{MaxItems: 2})
	require.NoError(t, err)
	assert.LessOrEqual(t, len(files), 2, "MaxItems must be respected")
}

// TestUS0305_GDriveModifiedTimeDecoded verifies RFC3339 time is decoded.
func TestUS0305_GDriveModifiedTimeDecoded(t *testing.T) {
	t.Parallel()

	fixed := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)
	srv := makeGDriveServer(t, []map[string]any{
		{"id": "f1", "name": "note.md", "mimeType": "text/markdown",
			"modifiedTime": fixed.Format(time.RFC3339)},
	})
	defer srv.Close()

	client := gdrive.NewClient(nil, srv.URL, "tok")
	files, err := client.ListFiles(context.Background(), gdrive.ListOptions{})
	require.NoError(t, err)
	require.Len(t, files, 1)
	assert.Equal(t, fixed, files[0].ModifiedTime)
}

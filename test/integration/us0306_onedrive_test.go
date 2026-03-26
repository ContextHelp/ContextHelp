package integration

// US-0306: OneDrive / Microsoft Graph import via mocked API.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/importer/onedrive"
)

func makeOneDriveServer(t *testing.T, items []map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"value": items,
		})
	}))
}

// TestUS0306_OneDriveListFolderItems verifies file list from mocked Graph API.
func TestUS0306_OneDriveListFolderItems(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC().Truncate(time.Second)
	items := []map[string]any{
		{
			"id":   "item-001",
			"name": "Meeting Notes.docx",
			"file": map[string]any{"mimeType": "application/vnd.openxmlformats-officedocument.wordprocessingml.document"},
			"size": float64(20480),
			"lastModifiedDateTime": now.Format(time.RFC3339),
			"webUrl": "https://onedrive.live.com/item-001",
			"parentReference": map[string]any{
				"driveId": "drive-abc",
				"path":    "/root:/Documents",
			},
		},
		{
			"id":   "item-002",
			"name": "Budget.xlsx",
			"file": map[string]any{"mimeType": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"},
			"size": float64(10240),
			"lastModifiedDateTime": now.Format(time.RFC3339),
			"webUrl": "https://onedrive.live.com/item-002",
			"parentReference": map[string]any{
				"driveId": "drive-abc",
				"path":    "/root:/Documents",
			},
		},
	}
	srv := makeOneDriveServer(t, items)
	defer srv.Close()

	client := onedrive.NewClient(nil, srv.URL, "test-token")
	got, err := client.ListDriveFolderItems(context.Background(), "drive-abc", "/", 0)
	require.NoError(t, err)
	require.Len(t, got, 2)

	assert.Equal(t, "item-001", got[0].ID)
	assert.Equal(t, "Meeting Notes.docx", got[0].Name)
	assert.True(t, got[0].IsFile, "must be marked as file")
}

// TestUS0306_OneDriveRequiresDriveID verifies validation.
func TestUS0306_OneDriveRequiresDriveID(t *testing.T) {
	t.Parallel()

	srv := makeOneDriveServer(t, nil)
	defer srv.Close()

	client := onedrive.NewClient(nil, srv.URL, "tok")
	_, err := client.ListDriveFolderItems(context.Background(), "", "/", 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "drive ID")
}

// TestUS0306_OneDriveRequiresToken verifies token validation.
func TestUS0306_OneDriveRequiresToken(t *testing.T) {
	t.Parallel()

	srv := makeOneDriveServer(t, nil)
	defer srv.Close()

	client := onedrive.NewClient(nil, srv.URL, "")
	_, err := client.ListDriveFolderItems(context.Background(), "drive-abc", "/", 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "token")
}

package dropbox

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewClientDefaults(t *testing.T) {
	c := NewClient(nil, "", "", "tok")
	if c.apiBase != defaultAPIBase {
		t.Errorf("apiBase: got %q, want %q", c.apiBase, defaultAPIBase)
	}
	if c.contentBase != defaultContentBase {
		t.Errorf("contentBase: got %q, want %q", c.contentBase, defaultContentBase)
	}
	if c.token != "tok" {
		t.Errorf("token: got %q, want tok", c.token)
	}
}

func TestListFilesRequiresToken(t *testing.T) {
	c := NewClient(nil, "", "", "")
	_, err := c.ListFiles(context.Background(), ListOptions{})
	if err == nil || !strings.Contains(err.Error(), "access token is required") {
		t.Fatalf("expected token error, got: %v", err)
	}
}

func TestListFilesSuccess(t *testing.T) {
	now := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	older := time.Date(2025, 12, 1, 0, 0, 0, 0, time.UTC)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			http.Error(w, "no auth", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/2/files/list_folder":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(listFolderResult{
				Entries: []listFolderEntry{
					{Tag: "file", ID: "id:f1", Name: "notes.md", PathDisplay: "/notes.md",
						Size: 100, ServerModified: now.Format("2006-01-02T15:04:05Z"),
						ContentHash: "hash1"},
					{Tag: "file", ID: "id:f2", Name: "old.txt", PathDisplay: "/old.txt",
						Size: 50, ServerModified: older.Format("2006-01-02T15:04:05Z"),
						ContentHash: "hash2"},
					{Tag: "folder", ID: "id:d1", Name: "docs", PathDisplay: "/docs"},
					{Tag: "deleted", ID: "", Name: "gone.txt", PathDisplay: "/gone.txt"},
				},
				Cursor:  "cursor-abc",
				HasMore: false,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	since := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c := NewClient(srv.Client(), srv.URL, srv.URL, "test-token")
	result, err := c.ListFiles(context.Background(), ListOptions{
		Path:  "/",
		Since: &since,
	})
	if err != nil {
		t.Fatalf("ListFiles: %v", err)
	}

	// Deleted entries filtered; old.txt filtered by Since; folder included.
	if len(result.Files) != 2 {
		t.Fatalf("expected 2 files (notes.md + docs folder), got %d", len(result.Files))
	}
	if result.Files[0].Name != "notes.md" {
		t.Errorf("file[0].Name: got %q, want notes.md", result.Files[0].Name)
	}
	if result.Files[0].ModifiedTime.IsZero() {
		t.Errorf("file[0].ModifiedTime: want non-zero")
	}
	if result.Files[1].IsFolder != true {
		t.Errorf("file[1].IsFolder: want true (docs)")
	}
	if result.Cursor != "cursor-abc" {
		t.Errorf("Cursor: got %q, want cursor-abc", result.Cursor)
	}
}

func TestListFilesMaxItems(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		entries := make([]listFolderEntry, 5)
		for i := range entries {
			entries[i] = listFolderEntry{
				Tag: "file", ID: "id:f" + string(rune('0'+i)),
				Name:        "file.txt",
				PathDisplay: "/file.txt",
			}
		}
		if r.URL.Path == "/2/files/list_folder" {
			json.NewEncoder(w).Encode(listFolderResult{
				Entries: entries,
				Cursor:  "c1",
				HasMore: false,
			})
		} else if r.URL.Path == "/2/files/list_folder/get_latest_cursor" {
			json.NewEncoder(w).Encode(latestCursorResult{Cursor: "c-latest"})
		} else {
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := NewClient(srv.Client(), srv.URL, srv.URL, "tok")
	result, err := c.ListFiles(context.Background(), ListOptions{MaxItems: 3})
	if err != nil {
		t.Fatalf("ListFiles: %v", err)
	}
	if len(result.Files) > 3 {
		t.Errorf("expected at most 3 files, got %d", len(result.Files))
	}
}

func TestListFilesCursorResume(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/2/files/list_folder/continue" {
			http.NotFound(w, r)
			return
		}
		var req map[string]string
		json.NewDecoder(r.Body).Decode(&req)
		if req["cursor"] != "saved-cursor" {
			http.Error(w, "wrong cursor", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(listFolderResult{
			Entries: []listFolderEntry{
				{Tag: "file", ID: "id:x1", Name: "new.md", PathDisplay: "/new.md"},
			},
			Cursor:  "cursor-next",
			HasMore: false,
		})
	}))
	defer srv.Close()

	c := NewClient(srv.Client(), srv.URL, srv.URL, "tok")
	result, err := c.ListFiles(context.Background(), ListOptions{Cursor: "saved-cursor"})
	if err != nil {
		t.Fatalf("ListFiles with cursor: %v", err)
	}
	if len(result.Files) != 1 || result.Files[0].Name != "new.md" {
		t.Errorf("unexpected files: %+v", result.Files)
	}
	if result.Cursor != "cursor-next" {
		t.Errorf("Cursor: got %q, want cursor-next", result.Cursor)
	}
}

func TestListFilesAuthError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := NewClient(srv.Client(), srv.URL, srv.URL, "bad-token")
	_, err := c.ListFiles(context.Background(), ListOptions{})
	if err == nil {
		t.Fatal("expected error for 401")
	}
	if !strings.Contains(err.Error(), "auth error") {
		t.Errorf("expected auth error, got: %v", err)
	}
}

func TestListFilesRateLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := NewClient(srv.Client(), srv.URL, srv.URL, "tok")
	_, err := c.ListFiles(context.Background(), ListOptions{})
	if err == nil {
		t.Fatal("expected error for 429")
	}
	if !strings.Contains(err.Error(), "rate limit") {
		t.Errorf("expected rate limit error, got: %v", err)
	}
}

func TestDownloadFileSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/2/files/download" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Dropbox-API-Arg") == "" {
			http.Error(w, "missing arg", http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("file content here"))
	}))
	defer srv.Close()

	c := NewClient(srv.Client(), srv.URL, srv.URL, "tok")
	data, err := c.DownloadFile(context.Background(), "/notes.md")
	if err != nil {
		t.Fatalf("DownloadFile: %v", err)
	}
	if string(data) != "file content here" {
		t.Errorf("content: got %q", string(data))
	}
}

func TestDownloadFileRequiresToken(t *testing.T) {
	c := NewClient(nil, "", "", "")
	_, err := c.DownloadFile(context.Background(), "/notes.md")
	if err == nil || !strings.Contains(err.Error(), "access token is required") {
		t.Fatalf("expected token error, got: %v", err)
	}
}

func TestDownloadFileRequiresPath(t *testing.T) {
	c := NewClient(nil, "", "", "tok")
	_, err := c.DownloadFile(context.Background(), "")
	if err == nil || !strings.Contains(err.Error(), "path is required") {
		t.Fatalf("expected path error, got: %v", err)
	}
}

func TestIsSupportedExtension(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"/docs/readme.md", true},
		{"/docs/README.MD", true},
		{"/report.pdf", true},
		{"/data.csv", true},
		{"/notes.txt", true},
		{"/photo.jpg", false},
		{"/video.mp4", false},
		{"/archive.zip", false},
	}
	for _, tc := range cases {
		got := IsSupportedExtension(tc.path)
		if got != tc.want {
			t.Errorf("IsSupportedExtension(%q): got %v, want %v", tc.path, got, tc.want)
		}
	}
}

func TestToFile(t *testing.T) {
	e := listFolderEntry{
		Tag:            "file",
		ID:             "id:abc",
		Name:           "test.md",
		PathDisplay:    "/test.md",
		Size:           1234,
		ServerModified: "2026-02-15T10:30:00Z",
		ContentHash:    "hashval",
	}
	f := toFile(e)
	if f.ID != "id:abc" {
		t.Errorf("ID: %q", f.ID)
	}
	if f.Path != "/test.md" {
		t.Errorf("Path: %q", f.Path)
	}
	if f.Size != 1234 {
		t.Errorf("Size: %d", f.Size)
	}
	if f.IsFolder {
		t.Errorf("IsFolder: want false for file tag")
	}
	if f.ModifiedTime.IsZero() {
		t.Error("ModifiedTime: want non-zero")
	}
}

package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	pinboardimporter "github.com/ideacrafterslabs/ctxt/internal/importer/pinboard"
)

type fakePinboardClient struct {
	fetchPosts func(ctx context.Context, opts pinboardimporter.FetchOptions) ([]pinboardimporter.Bookmark, error)
}

func (f *fakePinboardClient) FetchPosts(ctx context.Context, opts pinboardimporter.FetchOptions) ([]pinboardimporter.Bookmark, error) {
	if f.fetchPosts == nil {
		return nil, nil
	}
	return f.fetchPosts(ctx, opts)
}

func TestImportPinboardRequiresSource(t *testing.T) {
	t.Setenv(pinboardTokenEnv, "")

	_, err := executeCommand("import", "pinboard", "--dry-run")
	if err == nil {
		t.Fatal("expected missing source to fail")
	}
	if !strings.Contains(err.Error(), "pinboard source is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestImportPinboardInvalidSince(t *testing.T) {
	t.Setenv(pinboardTokenEnv, "test-token")

	_, err := executeCommand("import", "pinboard", "--since", "not-a-time", "--dry-run")
	if err == nil {
		t.Fatal("expected invalid --since to fail")
	}
	if !strings.Contains(err.Error(), "invalid --since") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestImportPinboardDryRunFromFileSelective(t *testing.T) {
	t.Setenv(pinboardTokenEnv, "")

	file := writeTempPinboardExport(t, `[
  {
    "href": "https://go.dev",
    "description": "Go",
    "extended": "Official Go website",
    "hash": "h1",
    "time": "2026-02-01T12:00:00Z",
    "shared": "yes",
    "toread": "no",
    "tags": "go language"
  },
  {
    "href": "https://example.com/js",
    "description": "JavaScript Notes",
    "extended": "JS note",
    "hash": "h2",
    "time": "2026-02-01T12:00:00Z",
    "shared": "no",
    "toread": "yes",
    "tags": "javascript"
  },
  {
    "href": "https://example.com/old-go",
    "description": "Old Go",
    "extended": "Old note",
    "hash": "h3",
    "time": "2025-01-01T12:00:00Z",
    "shared": "yes",
    "toread": "no",
    "tags": "go archive"
  }
]`)

	out, err := executeCommand(
		"import", "pinboard",
		"--file", file,
		"--since", "2026-01-01",
		"--tag", "go",
		"--dry-run",
	)
	if err != nil {
		t.Fatalf("import pinboard dry-run should succeed: %v", err)
	}

	if !strings.Contains(out, "Pinboard scope: source=file; since=2026-01-01T00:00:00Z; tags=go") {
		t.Fatalf("expected scope description, got:\n%s", out)
	}
	if !strings.Contains(out, "Pinboard bookmarks selected: 1 (dry-run)") {
		t.Fatalf("expected dry-run count, got:\n%s", out)
	}
	if !strings.Contains(out, "Scanned: 3, Skipped: 2") {
		t.Fatalf("expected scanned/skipped summary, got:\n%s", out)
	}
	if !strings.Contains(out, "Go -> https://go.dev") {
		t.Fatalf("expected preview line, got:\n%s", out)
	}
}

func TestImportPinboardEnqueueFromAPIAndFile(t *testing.T) {
	t.Setenv(pinboardTokenEnv, "test-token")

	file := writeTempPinboardExport(t, `[
  {
    "href": "https://example.com/file-only",
    "description": "File Only",
    "extended": "From file source",
    "hash": "file-1",
    "time": "2026-02-10T10:00:00Z",
    "shared": "yes",
    "toread": "no",
    "tags": "fromfile"
  },
  {
    "href": "https://example.com/duplicate",
    "description": "Duplicate Bookmark",
    "extended": "From file source duplicate",
    "hash": "dup-1",
    "time": "2026-02-11T10:00:00Z",
    "shared": "yes",
    "toread": "no",
    "tags": "common"
  }
]`)

	origFactory := newPinboardClient
	origEnqueue := enqueuePinboardItem
	t.Cleanup(func() {
		newPinboardClient = origFactory
		enqueuePinboardItem = origEnqueue
	})

	newPinboardClient = func(token, baseURL string) pinboardClient {
		return &fakePinboardClient{
			fetchPosts: func(ctx context.Context, opts pinboardimporter.FetchOptions) ([]pinboardimporter.Bookmark, error) {
				return []pinboardimporter.Bookmark{
					{
						URL:   "https://example.com/api-only",
						Title: "API Only",
						Hash:  "api-1",
						Tags:  []string{"fromapi"},
					},
					{
						URL:   "https://example.com/duplicate",
						Title: "Duplicate Bookmark API",
						Hash:  "dup-1", // duplicate by hash
						Tags:  []string{"common"},
					},
				}, nil
			},
		}
	}

	var enqueueCount int
	enqueuePinboardItem = func(serverURL, content, contentType, pipelineName, source string) (string, error) {
		enqueueCount++
		if contentType != "text" {
			return "", fmt.Errorf("expected text content, got %s", contentType)
		}
		if !strings.Contains(content, "Source: pinboard") {
			return "", fmt.Errorf("missing source metadata in payload")
		}
		return fmt.Sprintf("job-%d", enqueueCount), nil
	}

	out, err := executeCommand(
		"import", "pinboard",
		"--file", file,
		"--max-items", "2",
	)
	if err != nil {
		t.Fatalf("import pinboard enqueue should succeed: %v", err)
	}

	if enqueueCount != 2 {
		t.Fatalf("expected two enqueues (max-items), got %d", enqueueCount)
	}
	if !strings.Contains(out, "Pinboard scope: source=file+api; max-items=2") {
		t.Fatalf("expected merged scope, got:\n%s", out)
	}
	if !strings.Contains(out, "Selected: 2") {
		t.Fatalf("expected selected summary, got:\n%s", out)
	}
	if !strings.Contains(out, "Imported: 2") {
		t.Fatalf("expected imported summary, got:\n%s", out)
	}
}

func writeTempPinboardExport(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "pinboard.json")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write pinboard export fixture: %v", err)
	}
	return path
}

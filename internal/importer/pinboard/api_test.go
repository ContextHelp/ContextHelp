package pinboard

import (
	"strings"
	"testing"
	"time"
)

func TestParseExportArrayAndObjectForms(t *testing.T) {
	t.Parallel()

	arrayInput := []byte(`[
	  {"href":"https://example.com/a","description":"A","extended":"note","hash":"h1","time":"2026-02-10T10:00:00Z","tags":"go test","shared":"yes","toread":"no"}
	]`)
	arrayParsed, err := parseExport(arrayInput)
	if err != nil {
		t.Fatalf("parse array export: %v", err)
	}
	if len(arrayParsed) != 1 {
		t.Fatalf("expected 1 bookmark from array input, got %d", len(arrayParsed))
	}
	if arrayParsed[0].Title != "A" || arrayParsed[0].URL != "https://example.com/a" {
		t.Fatalf("unexpected bookmark from array input: %#v", arrayParsed[0])
	}

	objectInput := []byte(`{
	  "posts": [
	    {"href":"https://example.com/b","description":"B","extended":"","hash":"h2","time":"2026-02-10T10:00:00Z","tags":"go","shared":"no","toread":"yes"}
	  ]
	}`)
	objectParsed, err := parseExport(objectInput)
	if err != nil {
		t.Fatalf("parse object export: %v", err)
	}
	if len(objectParsed) != 1 {
		t.Fatalf("expected 1 bookmark from object input, got %d", len(objectParsed))
	}
	if objectParsed[0].Hash != "h2" || objectParsed[0].Shared {
		t.Fatalf("unexpected bookmark from object input: %#v", objectParsed[0])
	}
}

func TestFilterBookmarksSinceTagsAndMaxItems(t *testing.T) {
	t.Parallel()

	since := mustRFC3339(t, "2026-01-01T00:00:00Z")
	bookmarks := []Bookmark{
		{
			URL:   "https://example.com/go",
			Title: "Go",
			Time:  mustRFC3339(t, "2026-02-10T10:00:00Z"),
			Tags:  []string{"go", "lang"},
		},
		{
			URL:   "https://example.com/js",
			Title: "JS",
			Time:  mustRFC3339(t, "2026-02-10T10:00:00Z"),
			Tags:  []string{"javascript"},
		},
		{
			URL:   "https://example.com/old-go",
			Title: "Old Go",
			Time:  mustRFC3339(t, "2025-02-10T10:00:00Z"),
			Tags:  []string{"go"},
		},
	}

	filtered := FilterBookmarks(bookmarks, &since, []string{"go"}, 1)
	if len(filtered) != 1 {
		t.Fatalf("expected 1 bookmark after filters, got %d", len(filtered))
	}
	if filtered[0].Title != "Go" {
		t.Fatalf("unexpected filtered bookmark: %#v", filtered[0])
	}
}

func TestRenderContentIncludesCoreMetadata(t *testing.T) {
	t.Parallel()

	bookmark := Bookmark{
		URL:    "https://example.com",
		Title:  "Example",
		Notes:  "Important note",
		Tags:   []string{"go", "tools"},
		Time:   mustRFC3339(t, "2026-02-10T10:00:00Z"),
		Hash:   "abc123",
		Shared: true,
		ToRead: false,
	}

	out := RenderContent(bookmark)
	for _, want := range []string{
		"# Example",
		"URL: https://example.com",
		"Source: pinboard",
		"External ID: abc123",
		"Tags: go, tools",
		"Notes:",
		"Important note",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("rendered content missing %q:\n%s", want, out)
		}
	}
}

func mustRFC3339(t *testing.T, raw string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		t.Fatalf("parse time %q: %v", raw, err)
	}
	return parsed
}

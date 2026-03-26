package integration

// US-0301: Chrome bookmarks import (Netscape HTML format).

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/importer/chrome"
)

func testdataPath(elem ...string) string {
	_, file, _, _ := runtime.Caller(0)
	base := filepath.Join(filepath.Dir(file), "..", "testdata", "importers")
	return filepath.Join(append([]string{base}, elem...)...)
}

// TestUS0301_ChromeBookmarksParseHTML verifies the Chrome importer parses
// the Netscape HTML fixture and returns typed Bookmark records.
func TestUS0301_ChromeBookmarksParseHTML(t *testing.T) {
	t.Parallel()

	bmarks, err := chrome.ParseBookmarksFile(testdataPath("bookmarks.html"))
	require.NoError(t, err)
	require.NotEmpty(t, bmarks, "must parse at least one bookmark")

	assert.Equal(t, "https://example.com/article", bmarks[0].URL)
	assert.Equal(t, "Example Article", bmarks[0].Title)
}

// TestUS0301_ChromeBookmarksFolderPath verifies folder hierarchy is captured.
func TestUS0301_ChromeBookmarksFolderPath(t *testing.T) {
	t.Parallel()

	bmarks, err := chrome.ParseBookmarksFile(testdataPath("bookmarks.html"))
	require.NoError(t, err)
	require.NotEmpty(t, bmarks)

	// All bookmarks in the fixture sit under a named folder.
	for _, b := range bmarks {
		assert.NotEmpty(t, b.FolderPath, "bookmark %s must have a folder path", b.URL)
	}
}

// TestUS0301_ChromeBookmarksAddDate verifies ADD_DATE attribute is parsed.
func TestUS0301_ChromeBookmarksAddDate(t *testing.T) {
	t.Parallel()

	bmarks, err := chrome.ParseBookmarksFile(testdataPath("bookmarks.html"))
	require.NoError(t, err)
	require.NotEmpty(t, bmarks)

	assert.Greater(t, bmarks[0].AddDate, int64(0), "AddDate must be positive")
}

// TestUS0301_ChromeBookmarksFromBytes verifies in-memory parsing works too.
func TestUS0301_ChromeBookmarksFromBytes(t *testing.T) {
	t.Parallel()

	html := []byte(`<!DOCTYPE NETSCAPE-Bookmark-file-1>
<TITLE>Bookmarks</TITLE><H1>Bookmarks</H1>
<DL><p>
<DT><A HREF="https://golang.org" ADD_DATE="1700000001">Go</A>
<DT><A HREF="https://pkg.go.dev" ADD_DATE="1700000002">Pkg Go Dev</A>
</DL>`)

	bmarks, err := chrome.ParseBookmarks(html)
	require.NoError(t, err)
	assert.Len(t, bmarks, 2)
	assert.Equal(t, "https://golang.org", bmarks[0].URL)
	assert.Equal(t, "Go", bmarks[0].Title)
}

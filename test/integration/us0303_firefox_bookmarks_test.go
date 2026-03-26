package integration

// US-0303: Firefox bookmarks import (Netscape HTML format).

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/importer/firefox"
)

// firefoxBookmarksHTML is a minimal Firefox Netscape HTML bookmark export.
const firefoxBookmarksHTML = `<!DOCTYPE NETSCAPE-Bookmark-file-1>
<META HTTP-EQUIV="Content-Type" CONTENT="text/html; charset=UTF-8">
<TITLE>Bookmarks Menu</TITLE>
<H1>Bookmarks Menu</H1>
<DL><p>
    <DT><H3 ADD_DATE="1700000000">Mozilla Firefox</H3>
    <DL><p>
        <DT><A HREF="https://www.mozilla.org/en-US/firefox/central/" ADD_DATE="1700000001">Getting Started</A>
        <DT><A HREF="https://support.mozilla.org/" ADD_DATE="1700000002">Help and Tutorials</A>
    </DL><p>
    <DT><H3 ADD_DATE="1700000000">Personal Bookmarks</H3>
    <DL><p>
        <DT><A HREF="https://example.com/research" ADD_DATE="1700000010">Research Papers</A>
    </DL><p>
</DL>`

// TestUS0303_FirefoxBookmarksParseHTML verifies Netscape HTML parsing.
func TestUS0303_FirefoxBookmarksParseHTML(t *testing.T) {
	t.Parallel()

	bmarks, err := firefox.ParseBookmarks([]byte(firefoxBookmarksHTML))
	require.NoError(t, err)
	require.Len(t, bmarks, 3, "must parse 3 bookmarks")
}

// TestUS0303_FirefoxBookmarksFromFile verifies file-based parsing via shared fixture.
func TestUS0303_FirefoxBookmarksFromFile(t *testing.T) {
	t.Parallel()

	bmarks, err := firefox.ParseBookmarksFile(testdataPath("bookmarks.html"))
	require.NoError(t, err)
	require.NotEmpty(t, bmarks)

	// All bookmarks must have URL and Title set.
	for _, b := range bmarks {
		assert.NotEmpty(t, b.URL)
		assert.NotEmpty(t, b.Title)
	}
}

// TestUS0303_FirefoxBookmarksURLsExtracted verifies correct URL extraction.
func TestUS0303_FirefoxBookmarksURLsExtracted(t *testing.T) {
	t.Parallel()

	bmarks, err := firefox.ParseBookmarks([]byte(firefoxBookmarksHTML))
	require.NoError(t, err)

	assert.Equal(t, "https://www.mozilla.org/en-US/firefox/central/", bmarks[0].URL)
	assert.Equal(t, "Getting Started", bmarks[0].Title)
}

// TestUS0303_FirefoxBookmarksFolderAssigned verifies folder assignment.
func TestUS0303_FirefoxBookmarksFolderAssigned(t *testing.T) {
	t.Parallel()

	bmarks, err := firefox.ParseBookmarks([]byte(firefoxBookmarksHTML))
	require.NoError(t, err)

	assert.Equal(t, "Mozilla Firefox", bmarks[0].FolderPath)
	assert.Equal(t, "Personal Bookmarks", bmarks[2].FolderPath)
}

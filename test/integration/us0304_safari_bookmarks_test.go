package integration

// US-0304: Safari bookmarks import.
//
// Safari exports bookmarks in the same Netscape HTML format as Chrome/Firefox.
// Tested via the shared bookmarks package (aliased through safari package).

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/importer/safari"
)

// safariBookmarksHTML is a minimal Safari Netscape HTML bookmark export.
const safariBookmarksHTML = `<!DOCTYPE NETSCAPE-Bookmark-file-1>
<META HTTP-EQUIV="Content-Type" CONTENT="text/html; charset=UTF-8">
<TITLE>Bookmarks</TITLE>
<H1>Bookmarks</H1>
<DL><p>
    <DT><H3 ADD_DATE="1700000000">Favorites</H3>
    <DL><p>
        <DT><A HREF="https://www.apple.com" ADD_DATE="1700000001">Apple</A>
        <DT><A HREF="https://developer.apple.com" ADD_DATE="1700000002">Apple Developer</A>
    </DL><p>
    <DT><H3 ADD_DATE="1700000000">Reading List</H3>
    <DL><p>
        <DT><A HREF="https://webkit.org/blog/" ADD_DATE="1700000010">WebKit Blog</A>
    </DL><p>
</DL>`

// TestUS0304_SafariBookmarksParseHTML verifies Netscape HTML parsing.
func TestUS0304_SafariBookmarksParseHTML(t *testing.T) {
	t.Parallel()

	bmarks, err := safari.ParseBookmarks([]byte(safariBookmarksHTML))
	require.NoError(t, err)
	require.Len(t, bmarks, 3, "must parse 3 bookmarks")
}

// TestUS0304_SafariBookmarksFromFile verifies file-based parsing via shared fixture.
func TestUS0304_SafariBookmarksFromFile(t *testing.T) {
	t.Parallel()

	bmarks, err := safari.ParseBookmarksFile(testdataPath("bookmarks.html"))
	require.NoError(t, err)
	require.NotEmpty(t, bmarks)
}

// TestUS0304_SafariBookmarksURLsAndTitles verifies content extraction.
func TestUS0304_SafariBookmarksURLsAndTitles(t *testing.T) {
	t.Parallel()

	bmarks, err := safari.ParseBookmarks([]byte(safariBookmarksHTML))
	require.NoError(t, err)

	assert.Equal(t, "https://www.apple.com", bmarks[0].URL)
	assert.Equal(t, "Apple", bmarks[0].Title)
	assert.Equal(t, "Favorites", bmarks[0].FolderPath)
}

// TestUS0304_SafariBookmarksReadingList verifies Reading List folder captured.
func TestUS0304_SafariBookmarksReadingList(t *testing.T) {
	t.Parallel()

	bmarks, err := safari.ParseBookmarks([]byte(safariBookmarksHTML))
	require.NoError(t, err)
	require.Len(t, bmarks, 3)

	assert.Equal(t, "Reading List", bmarks[2].FolderPath)
	assert.Equal(t, "https://webkit.org/blog/", bmarks[2].URL)
}

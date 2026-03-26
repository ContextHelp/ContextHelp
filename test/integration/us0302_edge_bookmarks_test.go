package integration

// US-0302: Edge bookmarks import.
//
// Edge exports bookmarks in the same Netscape HTML format as Chrome.
// We use the shared bookmarks package via the chrome importer alias.

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/importer/bookmarks"
)

// edgeBookmarksHTML is a minimal Edge Netscape HTML bookmark export fixture.
const edgeBookmarksHTML = `<!DOCTYPE NETSCAPE-Bookmark-file-1>
<META HTTP-EQUIV="Content-Type" CONTENT="text/html; charset=UTF-8">
<TITLE>Bookmarks</TITLE>
<H1>Bookmarks bar</H1>
<DL><p>
    <DT><H3 ADD_DATE="1700000000">Favorites bar</H3>
    <DL><p>
        <DT><A HREF="https://microsoft.com" ADD_DATE="1700000010" LAST_MODIFIED="1700000020">Microsoft</A>
        <DT><A HREF="https://github.com/microsoft" ADD_DATE="1700000030">Microsoft GitHub</A>
    </DL><p>
    <DT><H3 ADD_DATE="1700000000">Other favorites</H3>
    <DL><p>
        <DT><A HREF="https://azure.microsoft.com" ADD_DATE="1700000040">Azure</A>
    </DL><p>
</DL>`

// TestUS0302_EdgeBookmarksParseHTML verifies the Edge Netscape HTML format parses correctly.
func TestUS0302_EdgeBookmarksParseHTML(t *testing.T) {
	t.Parallel()

	bmarks, err := bookmarks.ParseBookmarks([]byte(edgeBookmarksHTML))
	require.NoError(t, err)
	require.Len(t, bmarks, 3, "must parse 3 bookmarks from fixture")
}

// TestUS0302_EdgeBookmarksFolderHierarchy verifies nested folder paths.
func TestUS0302_EdgeBookmarksFolderHierarchy(t *testing.T) {
	t.Parallel()

	bmarks, err := bookmarks.ParseBookmarks([]byte(edgeBookmarksHTML))
	require.NoError(t, err)

	// First two bookmarks are under "Favorites bar".
	assert.Equal(t, "Favorites bar", bmarks[0].FolderPath)
	assert.Equal(t, "Favorites bar", bmarks[1].FolderPath)
	// Third bookmark is under "Other favorites".
	assert.Equal(t, "Other favorites", bmarks[2].FolderPath)
}

// TestUS0302_EdgeBookmarksURLAndTitle verifies URL and Title are extracted.
func TestUS0302_EdgeBookmarksURLAndTitle(t *testing.T) {
	t.Parallel()

	bmarks, err := bookmarks.ParseBookmarks([]byte(edgeBookmarksHTML))
	require.NoError(t, err)

	assert.Equal(t, "https://microsoft.com", bmarks[0].URL)
	assert.Equal(t, "Microsoft", bmarks[0].Title)
}

// TestUS0302_EdgeBookmarksLastModified verifies LAST_MODIFIED is captured.
func TestUS0302_EdgeBookmarksLastModified(t *testing.T) {
	t.Parallel()

	bmarks, err := bookmarks.ParseBookmarks([]byte(edgeBookmarksHTML))
	require.NoError(t, err)

	assert.Equal(t, int64(1700000020), bmarks[0].LastModified)
	// Bookmark without LAST_MODIFIED should be zero.
	assert.Equal(t, int64(0), bmarks[1].LastModified)
}

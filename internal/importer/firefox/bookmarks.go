package firefox

import (
	bookmarksimporter "github.com/ideacrafterslabs/ctxt/internal/importer/bookmarks"
)

// Bookmark represents a single bookmark item from a Firefox export.
//
// Firefox bookmark exports use the same Netscape HTML format as Chrome/Edge.
// We keep a dedicated Firefox importer package for clearer source layout while
// reusing the shared parser behavior.
type Bookmark = bookmarksimporter.Bookmark

// ParseBookmarksFile parses a Firefox bookmarks HTML export file.
func ParseBookmarksFile(path string) ([]Bookmark, error) {
	return bookmarksimporter.ParseBookmarksFile(path)
}

// ParseBookmarks parses Firefox bookmarks from Netscape bookmark HTML.
func ParseBookmarks(data []byte) ([]Bookmark, error) {
	return bookmarksimporter.ParseBookmarks(data)
}

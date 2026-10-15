// Package chrome imports Chrome bookmark HTML exports.
package chrome

import (
	bookmarksimporter "github.com/ideacrafterslabs/ctxt/internal/importer/bookmarks"
)

// Bookmark represents a single bookmark item from a Chrome export.
//
// Chrome bookmark exports use the same Netscape HTML format as other browsers.
// We keep a dedicated Chrome importer package for clearer source layout while
// reusing the shared parser behavior.
type Bookmark = bookmarksimporter.Bookmark

// ParseBookmarksFile parses a Chrome bookmarks HTML export file.
func ParseBookmarksFile(path string) ([]Bookmark, error) {
	return bookmarksimporter.ParseBookmarksFile(path)
}

// ParseBookmarks parses Chrome bookmarks from Netscape bookmark HTML.
func ParseBookmarks(data []byte) ([]Bookmark, error) {
	return bookmarksimporter.ParseBookmarks(data)
}

package firefox

import "testing"

func TestParseBookmarksSharedFormat(t *testing.T) {
	input := `<!DOCTYPE NETSCAPE-Bookmark-file-1>
<TITLE>Bookmarks</TITLE>
<H1>Bookmarks</H1>
<DL><p>
    <DT><H3>Bookmarks Toolbar</H3>
    <DL><p>
        <DT><A HREF="https://www.mozilla.org">Mozilla</A>
    </DL><p>
</DL><p>`

	got, err := ParseBookmarks([]byte(input))
	if err != nil {
		t.Fatalf("ParseBookmarks returned error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 bookmark, got %d", len(got))
	}
	if got[0].URL != "https://www.mozilla.org" {
		t.Fatalf("unexpected URL: %q", got[0].URL)
	}
	if got[0].FolderPath != "Bookmarks Toolbar" {
		t.Fatalf("unexpected folder path: %q", got[0].FolderPath)
	}
}

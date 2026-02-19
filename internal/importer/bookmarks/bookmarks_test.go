package bookmarks

import (
	"strings"
	"testing"
)

func TestParseBookmarksNestedFolders(t *testing.T) {
	input := `<!DOCTYPE NETSCAPE-Bookmark-file-1>
<TITLE>Bookmarks</TITLE>
<H1>Bookmarks</H1>
<DL><p>
    <DT><H3 ADD_DATE="1700000000" LAST_MODIFIED="1700000001">Bookmarks Bar</H3>
    <DL><p>
        <DT><A HREF="https://example.com" ADD_DATE="1700000002">Example</A>
        <DT><H3>Reading List</H3>
        <DL><p>
            <DT><A HREF='https://go.dev/doc/' ADD_DATE='1700000003'>Go &amp; Docs</A>
        </DL><p>
    </DL><p>
</DL><p>`

	got, err := ParseBookmarks([]byte(input))
	if err != nil {
		t.Fatalf("ParseBookmarks returned error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 bookmarks, got %d", len(got))
	}

	if got[0].URL != "https://example.com" {
		t.Errorf("bookmark 0 URL = %q", got[0].URL)
	}
	if got[0].Title != "Example" {
		t.Errorf("bookmark 0 title = %q", got[0].Title)
	}
	if got[0].FolderPath != "Bookmarks Bar" {
		t.Errorf("bookmark 0 folder path = %q", got[0].FolderPath)
	}
	if got[0].AddDate != 1700000002 {
		t.Errorf("bookmark 0 add date = %d", got[0].AddDate)
	}

	if got[1].URL != "https://go.dev/doc/" {
		t.Errorf("bookmark 1 URL = %q", got[1].URL)
	}
	if got[1].Title != "Go & Docs" {
		t.Errorf("bookmark 1 title = %q", got[1].Title)
	}
	if got[1].FolderPath != "Bookmarks Bar/Reading List" {
		t.Errorf("bookmark 1 folder path = %q", got[1].FolderPath)
	}
}

func TestParseBookmarksSkipsMissingHref(t *testing.T) {
	input := `<DL><p>
<DT><A ADD_DATE="1">No URL</A>
<DT><A HREF="https://valid.example" ADD_DATE="2">Valid</A>
</DL><p>`

	got, err := ParseBookmarks([]byte(input))
	if err != nil {
		t.Fatalf("ParseBookmarks returned error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 bookmark, got %d", len(got))
	}
	if got[0].URL != "https://valid.example" {
		t.Errorf("URL = %q", got[0].URL)
	}
}

func TestParseBookmarksEmptyInput(t *testing.T) {
	_, err := ParseBookmarks([]byte("   \n"))
	if err == nil {
		t.Fatal("expected error for empty input")
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Fatalf("expected empty input error, got: %v", err)
	}
}

func TestParseTagAttrSupportsQuotedAndUnquoted(t *testing.T) {
	tag := `<A HREF="https://a.example" DATA=raw LAST_MODIFIED='123'>x</A>`
	if got := parseTagAttr(tag, "href"); got != "https://a.example" {
		t.Fatalf("href = %q", got)
	}
	if got := parseTagAttr(tag, "data"); got != "raw" {
		t.Fatalf("data = %q", got)
	}
	if got := parseTagAttr(tag, "last_modified"); got != "123" {
		t.Fatalf("last_modified = %q", got)
	}
}

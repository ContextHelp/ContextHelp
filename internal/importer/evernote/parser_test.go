package evernote

import (
	"strings"
	"testing"
)

const sampleENEX = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE en-export SYSTEM "http://xml.evernote.com/pub/evernote-export4.dtd">
<en-export export-date="20260218T010203Z" application="Evernote" version="10.x">
  <note>
    <guid>note-guid-1</guid>
    <title>Project Notes</title>
    <content><![CDATA[<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE en-note SYSTEM "http://xml.evernote.com/pub/enml2.dtd">
<en-note><div>Hello <b>team</b>.</div><div>Next steps<br/>- Ship</div></en-note>]]></content>
    <created>20260110T090000Z</created>
    <updated>20260115T100000Z</updated>
    <tag>Work</tag>
    <tag>Roadmap</tag>
    <note-attributes>
      <source-url>https://example.com/project</source-url>
    </note-attributes>
    <resource></resource>
  </note>
  <note>
    <title>Personal</title>
    <content><![CDATA[<en-note><div>Buy milk</div></en-note>]]></content>
    <created>20251201T080000Z</created>
    <updated>20251205T080000Z</updated>
    <tag>Personal</tag>
  </note>
</en-export>`

func TestParseENEX(t *testing.T) {
	notes, err := ParseENEX([]byte(sampleENEX))
	if err != nil {
		t.Fatalf("ParseENEX error: %v", err)
	}
	if len(notes) != 2 {
		t.Fatalf("expected 2 notes, got %d", len(notes))
	}

	n0 := notes[0]
	if n0.ExternalID != "note-guid-1" {
		t.Fatalf("expected guid external id, got %q", n0.ExternalID)
	}
	if n0.Title != "Project Notes" {
		t.Fatalf("unexpected title: %q", n0.Title)
	}
	if !strings.Contains(n0.Content, "Hello team.") || !strings.Contains(n0.Content, "Next steps") {
		t.Fatalf("unexpected content:\n%s", n0.Content)
	}
	if n0.SourceURL != "https://example.com/project" {
		t.Fatalf("unexpected source url: %q", n0.SourceURL)
	}
	if len(n0.Tags) != 2 {
		t.Fatalf("expected 2 tags, got %d", len(n0.Tags))
	}
	if n0.ResourceCount != 1 {
		t.Fatalf("expected one resource, got %d", n0.ResourceCount)
	}

	n1 := notes[1]
	if n1.ExternalID == "" || n1.ExternalID == "note-guid-1" {
		t.Fatalf("expected generated external id, got %q", n1.ExternalID)
	}
	if n1.Updated.IsZero() {
		t.Fatalf("expected updated timestamp parsed")
	}
}

func TestParseExportHTML(t *testing.T) {
	html := `<!DOCTYPE html><html><head><title>Evernote Export</title></head><body><h1>Hello</h1><p>From HTML export.</p></body></html>`
	notes, err := ParseExport([]byte(html), "notes.html")
	if err != nil {
		t.Fatalf("ParseExport html error: %v", err)
	}
	if len(notes) != 1 {
		t.Fatalf("expected 1 note, got %d", len(notes))
	}
	if notes[0].Title != "Evernote Export" {
		t.Fatalf("unexpected title: %q", notes[0].Title)
	}
	if !strings.Contains(notes[0].Content, "From HTML export.") {
		t.Fatalf("unexpected content: %q", notes[0].Content)
	}
}

func TestParseExportEmpty(t *testing.T) {
	_, err := ParseExport([]byte("  \n"), "")
	if err == nil {
		t.Fatal("expected empty input error")
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRenderContent(t *testing.T) {
	out := RenderContent(Note{
		ExternalID:    "note-guid-1",
		Title:         "Project Notes",
		Content:       "Line one\nLine two",
		SourceURL:     "https://example.com/project",
		ResourceCount: 2,
		Tags:          []string{"Work", "Roadmap"},
	})

	for _, expected := range []string{
		"# Project Notes",
		"Source: https://example.com/project",
		"Provider: Evernote",
		"External ID: note-guid-1",
		"Tags: Work, Roadmap",
		"Resources: 2",
		"Line two",
	} {
		if !strings.Contains(out, expected) {
			t.Fatalf("expected %q in rendered content:\n%s", expected, out)
		}
	}
}

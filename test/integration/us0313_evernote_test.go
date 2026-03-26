package integration

// US-0313: Evernote ENEX export import.

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/importer/evernote"
)

// TestUS0313_EvernoteParseENEXFile verifies the ENEX fixture is parsed end-to-end.
func TestUS0313_EvernoteParseENEXFile(t *testing.T) {
	t.Parallel()

	notes, err := evernote.ParseExportFile(testdataPath("evernote", "export.enex"))
	require.NoError(t, err)
	require.Len(t, notes, 2, "fixture contains 2 notes")
}

// TestUS0313_EvernoteNoteFields verifies all expected fields are populated.
func TestUS0313_EvernoteNoteFields(t *testing.T) {
	t.Parallel()

	notes, err := evernote.ParseExportFile(testdataPath("evernote", "export.enex"))
	require.NoError(t, err)
	require.NotEmpty(t, notes)

	first := notes[0]
	assert.NotEmpty(t, first.ExternalID, "ExternalID must be set")
	assert.Equal(t, "Test Note One", first.Title, "Title must be extracted")
	assert.NotEmpty(t, first.Content, "Content must be extracted from CDATA")
	assert.False(t, first.Created.IsZero(), "Created timestamp must be parsed")
	assert.False(t, first.Updated.IsZero(), "Updated timestamp must be parsed")
}

// TestUS0313_EvernoteTagsExtracted verifies tag extraction.
func TestUS0313_EvernoteTagsExtracted(t *testing.T) {
	t.Parallel()

	notes, err := evernote.ParseExportFile(testdataPath("evernote", "export.enex"))
	require.NoError(t, err)
	require.NotEmpty(t, notes)

	// First note has tags: golang, testing
	assert.NotEmpty(t, notes[0].Tags, "tags must be extracted")
	assert.Contains(t, notes[0].Tags, "golang")
	assert.Contains(t, notes[0].Tags, "testing")

	// Second note has no tags.
	assert.Empty(t, notes[1].Tags)
}

// TestUS0313_EvernoteSourceURL verifies source URL extraction.
func TestUS0313_EvernoteSourceURL(t *testing.T) {
	t.Parallel()

	notes, err := evernote.ParseExportFile(testdataPath("evernote", "export.enex"))
	require.NoError(t, err)
	require.NotEmpty(t, notes)

	assert.Equal(t, "https://example.com/article", notes[0].SourceURL)
}

// TestUS0313_EvernoteRenderContent verifies RenderContent produces output.
func TestUS0313_EvernoteRenderContent(t *testing.T) {
	t.Parallel()

	notes, err := evernote.ParseExportFile(testdataPath("evernote", "export.enex"))
	require.NoError(t, err)
	require.NotEmpty(t, notes)

	for _, n := range notes {
		rendered := evernote.RenderContent(n)
		assert.NotEmpty(t, rendered, "RenderContent must produce non-empty string for %q", n.Title)
		assert.Contains(t, rendered, n.Title, "rendered content must include note title")
	}
}

// TestUS0313_EvernoteParseFromBytes verifies in-memory ENEX parsing.
func TestUS0313_EvernoteParseFromBytes(t *testing.T) {
	t.Parallel()

	enex := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE en-export SYSTEM "http://xml.evernote.com/pub/evernote-export4.dtd">
<en-export>
  <note>
    <title>Quick Note</title>
    <created>20260110T080000Z</created>
    <updated>20260110T080000Z</updated>
    <content><![CDATA[<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE en-note SYSTEM "http://xml.evernote.com/pub/enml2.dtd">
<en-note><p>Quick in-memory note.</p></en-note>]]></content>
  </note>
</en-export>`)

	notes, err := evernote.ParseENEX(enex)
	require.NoError(t, err)
	require.Len(t, notes, 1)
	assert.Equal(t, "Quick Note", notes[0].Title)
	assert.NotEmpty(t, notes[0].Content)
}

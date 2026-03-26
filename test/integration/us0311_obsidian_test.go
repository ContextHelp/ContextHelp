package integration

// US-0311: Obsidian vault import (folder of .md files).

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/importer/obsidian"
)

// TestUS0311_ObsidianWalkVault verifies the vault walker finds all markdown notes.
func TestUS0311_ObsidianWalkVault(t *testing.T) {
	t.Parallel()

	notes, err := obsidian.WalkVault(testdataPath("obsidian"))
	require.NoError(t, err)
	require.Len(t, notes, 2, "fixture vault contains exactly 2 notes")
}

// TestUS0311_ObsidianNoteFields verifies all expected fields are populated.
func TestUS0311_ObsidianNoteFields(t *testing.T) {
	t.Parallel()

	notes, err := obsidian.WalkVault(testdataPath("obsidian"))
	require.NoError(t, err)
	require.NotEmpty(t, notes)

	// Find note-one.md
	var note *obsidian.Note
	for i := range notes {
		if notes[i].RelPath == "note-one.md" {
			note = &notes[i]
			break
		}
	}
	require.NotNil(t, note, "note-one.md must be found in vault")

	assert.NotEmpty(t, note.ExternalID, "ExternalID must be set")
	assert.Equal(t, "My First Note", note.Title, "Title must come from frontmatter")
	assert.NotEmpty(t, note.Content, "Content must be set")
	assert.NotEmpty(t, note.ContentHash, "ContentHash must be computed")
	assert.False(t, note.ModTime.IsZero(), "ModTime must be set")
}

// TestUS0311_ObsidianFrontmatterParsed verifies YAML frontmatter extraction.
func TestUS0311_ObsidianFrontmatterParsed(t *testing.T) {
	t.Parallel()

	notes, err := obsidian.WalkVault(testdataPath("obsidian"))
	require.NoError(t, err)

	var note *obsidian.Note
	for i := range notes {
		if notes[i].RelPath == "note-one.md" {
			note = &notes[i]
			break
		}
	}
	require.NotNil(t, note)

	assert.NotEmpty(t, note.Frontmatter, "frontmatter must be parsed")
}

// TestUS0311_ObsidianTagsExtracted verifies inline and frontmatter tags.
func TestUS0311_ObsidianTagsExtracted(t *testing.T) {
	t.Parallel()

	notes, err := obsidian.WalkVault(testdataPath("obsidian"))
	require.NoError(t, err)

	var note *obsidian.Note
	for i := range notes {
		if notes[i].RelPath == "note-one.md" {
			note = &notes[i]
			break
		}
	}
	require.NotNil(t, note)

	assert.NotEmpty(t, note.Tags, "tags must be extracted")
	// Fixture has frontmatter tags: golang, testing; inline: #important
	assert.Contains(t, note.Tags, "golang")
	assert.Contains(t, note.Tags, "testing")
}

// TestUS0311_ObsidianWikilinksExtracted verifies [[wikilinks]] parsing.
func TestUS0311_ObsidianWikilinksExtracted(t *testing.T) {
	t.Parallel()

	notes, err := obsidian.WalkVault(testdataPath("obsidian"))
	require.NoError(t, err)

	var note *obsidian.Note
	for i := range notes {
		if notes[i].RelPath == "note-one.md" {
			note = &notes[i]
			break
		}
	}
	require.NotNil(t, note)

	assert.NotEmpty(t, note.Wikilinks, "wikilinks must be extracted")
	assert.Contains(t, note.Wikilinks, "Second Note")
}

// TestUS0311_ObsidianRenderContent verifies RenderContent output.
func TestUS0311_ObsidianRenderContent(t *testing.T) {
	t.Parallel()

	notes, err := obsidian.WalkVault(testdataPath("obsidian"))
	require.NoError(t, err)
	require.NotEmpty(t, notes)

	for _, n := range notes {
		rendered := obsidian.RenderContent(n)
		assert.NotEmpty(t, rendered, "RenderContent must not return empty string")
	}
}

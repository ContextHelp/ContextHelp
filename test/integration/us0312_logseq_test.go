package integration

// US-0312: Logseq graph import (pages + journals markdown).

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/importer/logseq"
)

// TestUS0312_LogseqWalkGraph verifies the graph walker finds all pages.
func TestUS0312_LogseqWalkGraph(t *testing.T) {
	t.Parallel()

	pages, err := logseq.WalkGraph(testdataPath("logseq-graph"))
	require.NoError(t, err)
	require.NotEmpty(t, pages, "must find pages in logseq fixture graph")
}

// TestUS0312_LogseqPageFields verifies page fields are populated.
func TestUS0312_LogseqPageFields(t *testing.T) {
	t.Parallel()

	pages, err := logseq.WalkGraph(testdataPath("logseq-graph"))
	require.NoError(t, err)

	var page *logseq.Page
	for i := range pages {
		if pages[i].Kind == logseq.PageKindPage {
			page = &pages[i]
			break
		}
	}
	require.NotNil(t, page, "must find at least one page-kind entry")

	assert.NotEmpty(t, page.ExternalID, "ExternalID must be set")
	assert.NotEmpty(t, page.Title, "Title must be set")
	assert.NotEmpty(t, page.Content, "Content must be set")
	assert.False(t, page.ModTime.IsZero(), "ModTime must be set")
}

// TestUS0312_LogseqJournalDetected verifies journal pages are classified.
func TestUS0312_LogseqJournalDetected(t *testing.T) {
	t.Parallel()

	pages, err := logseq.WalkGraph(testdataPath("logseq-graph"))
	require.NoError(t, err)

	var journal *logseq.Page
	for i := range pages {
		if pages[i].Kind == logseq.PageKindJournal {
			journal = &pages[i]
			break
		}
	}
	require.NotNil(t, journal, "must find at least one journal entry")

	assert.NotEmpty(t, journal.JournalDate, "JournalDate must be set for journal pages")
	assert.Equal(t, "2026-01-15", journal.JournalDate)
}

// TestUS0312_LogseqPagePropertiesExtracted verifies key:: value properties.
func TestUS0312_LogseqPagePropertiesExtracted(t *testing.T) {
	t.Parallel()

	pages, err := logseq.WalkGraph(testdataPath("logseq-graph"))
	require.NoError(t, err)

	var page *logseq.Page
	for i := range pages {
		if pages[i].Kind == logseq.PageKindPage {
			page = &pages[i]
			break
		}
	}
	require.NotNil(t, page)
	assert.NotEmpty(t, page.Properties, "page properties must be extracted")
}

// TestUS0312_LogseqRenderContent verifies RenderContent produces output.
func TestUS0312_LogseqRenderContent(t *testing.T) {
	t.Parallel()

	pages, err := logseq.WalkGraph(testdataPath("logseq-graph"))
	require.NoError(t, err)
	require.NotEmpty(t, pages)

	for _, p := range pages {
		rendered := logseq.RenderContent(p)
		assert.NotEmpty(t, rendered, "RenderContent must produce non-empty string for %s", p.RelPath)
	}
}

// TestUS0312_LogseqStableExternalID verifies ExternalID is stable across runs.
func TestUS0312_LogseqStableExternalID(t *testing.T) {
	t.Parallel()

	pages1, err := logseq.WalkGraph(testdataPath("logseq-graph"))
	require.NoError(t, err)
	pages2, err := logseq.WalkGraph(testdataPath("logseq-graph"))
	require.NoError(t, err)

	require.Equal(t, len(pages1), len(pages2))
	for i := range pages1 {
		assert.Equal(t, pages1[i].ExternalID, pages2[i].ExternalID,
			"ExternalID must be stable across runs for %s", pages1[i].RelPath)
	}
}

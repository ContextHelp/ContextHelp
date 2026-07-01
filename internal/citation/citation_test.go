package citation_test

import (
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/citation"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	uri "hop.top/cite/scheme"
)

// ---------------------------------------------------------------------------
// ParseCitations
// ---------------------------------------------------------------------------

func TestParseCitations_Single(t *testing.T) {
	content := "The team decided to migrate [ref:o-abc123]."
	got := citation.ParseCitations(content)
	require.Len(t, got, 1)
	assert.Equal(t, []string{"o-abc123"}, got[0].IDs)
	assert.Equal(t, "", got[0].Anchor)
	assert.Equal(t, 28, got[0].Position)
}

func TestParseCitations_Multiple(t *testing.T) {
	content := "Driven by market timing [ref:o-def456,o-ghi789]."
	got := citation.ParseCitations(content)
	require.Len(t, got, 1)
	assert.Equal(t, []string{"o-def456", "o-ghi789"}, got[0].IDs)
}

func TestParseCitations_WithAnchor(t *testing.T) {
	content := "The rationale [ref:o-abc123#rationale]."
	got := citation.ParseCitations(content)
	require.Len(t, got, 1)
	assert.Equal(t, []string{"o-abc123"}, got[0].IDs)
	assert.Equal(t, "rationale", got[0].Anchor)
}

func TestParseCitations_ManyCitations(t *testing.T) {
	content := "First claim [ref:o-aaa111]. Second claim [ref:o-bbb222]. Third [ref:o-ccc333,o-ddd444]."
	got := citation.ParseCitations(content)
	require.Len(t, got, 3)
	assert.Equal(t, []string{"o-aaa111"}, got[0].IDs)
	assert.Equal(t, []string{"o-bbb222"}, got[1].IDs)
	assert.Equal(t, []string{"o-ccc333", "o-ddd444"}, got[2].IDs)
}

func TestParseCitations_NoCitations(t *testing.T) {
	content := "No references in this content."
	got := citation.ParseCitations(content)
	assert.Empty(t, got)
}

func TestParseCitations_Empty(t *testing.T) {
	got := citation.ParseCitations("")
	assert.Empty(t, got)
}

func TestParseCitations_Position(t *testing.T) {
	content := "[ref:o-abc123]"
	got := citation.ParseCitations(content)
	require.Len(t, got, 1)
	assert.Equal(t, 0, got[0].Position)
}

// ---------------------------------------------------------------------------
// ValidateCitations
// ---------------------------------------------------------------------------

func makeObjectMap(ids ...string) map[string]*storage.KnowledgeObject {
	now := time.Now()
	m := make(map[string]*storage.KnowledgeObject)
	for _, id := range ids {
		m[id] = &storage.KnowledgeObject{ID: id, Type: "note", CreatedAt: now, UpdatedAt: now}
	}
	return m
}

func TestValidateCitations_AllValid(t *testing.T) {
	cits := []citation.Citation{
		{IDs: []string{"o-abc123"}},
		{IDs: []string{"o-def456", "o-ghi789"}},
	}
	objMap := makeObjectMap("o-abc123", "o-def456", "o-ghi789")
	errs := citation.ValidateCitations(cits, objMap)
	assert.Empty(t, errs)
}

func TestValidateCitations_InvalidID(t *testing.T) {
	cits := []citation.Citation{
		{IDs: []string{"o-missing"}},
	}
	objMap := makeObjectMap("o-abc123")
	errs := citation.ValidateCitations(cits, objMap)
	require.Len(t, errs, 1)
	assert.Contains(t, errs[0].Error(), "o-missing")
}

func TestValidateCitations_DuplicateIDOnlyReportedOnce(t *testing.T) {
	cits := []citation.Citation{
		{IDs: []string{"o-missing"}},
		{IDs: []string{"o-missing"}},
	}
	objMap := makeObjectMap("o-abc123")
	errs := citation.ValidateCitations(cits, objMap)
	// Duplicate IDs should only be reported once.
	require.Len(t, errs, 1)
}

func TestValidateCitations_Empty(t *testing.T) {
	errs := citation.ValidateCitations(nil, makeObjectMap("o-abc123"))
	assert.Empty(t, errs)
}

// ---------------------------------------------------------------------------
// BuildReferenceTable
// ---------------------------------------------------------------------------

func TestBuildReferenceTable_Basic(t *testing.T) {
	now := time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)
	objMap := map[string]*storage.KnowledgeObject{
		"o-abc123": {
			ID:        "o-abc123",
			Type:      "decision",
			Summaries: []string{"Defer infrastructure refactor"},
			Source:    "engineering-meeting.pdf",
			CreatedAt: now,
			UpdatedAt: now,
		},
		"o-def456": {
			ID:        "o-def456",
			Type:      "note",
			Summaries: []string{"Market timing analysis"},
			Source:    "slack-#engineering",
			CreatedAt: now,
			UpdatedAt: now,
		},
	}
	cits := []citation.Citation{
		{IDs: []string{"o-abc123"}},
		{IDs: []string{"o-def456"}},
	}
	table := citation.BuildReferenceTable(cits, objMap)
	assert.Contains(t, table, "## References")
	assert.Contains(t, table, "o-abc123")
	assert.Contains(t, table, "decision")
	assert.Contains(t, table, "Defer infrastructure refactor")
	assert.Contains(t, table, "engineering-meeting.pdf")
	assert.Contains(t, table, "2025-01-15")
	assert.Contains(t, table, "o-def456")
}

func TestBuildReferenceTable_DeduplicatesIDs(t *testing.T) {
	now := time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)
	objMap := map[string]*storage.KnowledgeObject{
		"o-abc123": {
			ID:        "o-abc123",
			Type:      "note",
			Summaries: []string{"Some note"},
			Source:    "test",
			CreatedAt: now,
			UpdatedAt: now,
		},
	}
	// Same ID cited twice.
	cits := []citation.Citation{
		{IDs: []string{"o-abc123"}},
		{IDs: []string{"o-abc123"}},
	}
	table := citation.BuildReferenceTable(cits, objMap)
	// Should appear exactly once.
	count := 0
	for i := 0; i < len(table); i++ {
		if i+len("o-abc123") <= len(table) && table[i:i+len("o-abc123")] == "o-abc123" {
			count++
		}
	}
	assert.Equal(t, 1, count, "ID should appear exactly once in reference table")
}

func TestBuildReferenceTable_TruncatesLongSummary(t *testing.T) {
	now := time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)
	longSummary := "This is a very long summary that exceeds the fifty character limit for display in the reference table"
	objMap := map[string]*storage.KnowledgeObject{
		"o-abc123": {
			ID:        "o-abc123",
			Type:      "note",
			Summaries: []string{longSummary},
			Source:    "test",
			CreatedAt: now,
			UpdatedAt: now,
		},
	}
	cits := []citation.Citation{{IDs: []string{"o-abc123"}}}
	table := citation.BuildReferenceTable(cits, objMap)
	assert.Contains(t, table, "...")
	assert.NotContains(t, table, longSummary)
}

func TestBuildReferenceTable_SkipsMissingObjects(t *testing.T) {
	objMap := map[string]*storage.KnowledgeObject{}
	cits := []citation.Citation{{IDs: []string{"o-missing"}}}
	table := citation.BuildReferenceTable(cits, objMap)
	// Should still produce a reference table header, just no row.
	assert.Contains(t, table, "## References")
	assert.NotContains(t, table, "o-missing")
}

func TestBuildReferenceTable_NoSummary(t *testing.T) {
	now := time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)
	objMap := map[string]*storage.KnowledgeObject{
		"o-abc123": {
			ID:         "o-abc123",
			Type:       "note",
			RawContent: "Some raw content for the object",
			Source:     "test",
			CreatedAt:  now,
			UpdatedAt:  now,
		},
	}
	cits := []citation.Citation{{IDs: []string{"o-abc123"}}}
	table := citation.BuildReferenceTable(cits, objMap)
	assert.Contains(t, table, "o-abc123")
}

// ---------------------------------------------------------------------------
// EnrichCitationsWithEntities
// ---------------------------------------------------------------------------

func TestEnrichCitationsWithEntities(t *testing.T) {
	now := time.Now()
	objMap := map[string]*storage.KnowledgeObject{
		"o-abc123": {
			ID: "o-abc123",
			Mentions: []uri.URI{
				{Scheme: "ctxt", Namespace: "entity", ID: "team/alice"},
				{Scheme: "ctxt", Namespace: "entity", ID: "project/api"},
			},
			CreatedAt: now,
			UpdatedAt: now,
		},
	}
	cits := []citation.Citation{
		{IDs: []string{"o-abc123"}},
	}
	citation.EnrichCitationsWithEntities(cits, objMap)
	assert.Equal(t, []string{"ctxt://entity/team/alice", "ctxt://entity/project/api"}, cits[0].Entities)
}

func TestEnrichCitationsWithEntities_MultipleCitations(t *testing.T) {
	now := time.Now()
	objMap := map[string]*storage.KnowledgeObject{
		"o-aaa": {ID: "o-aaa", Mentions: []uri.URI{{Scheme: "ctxt", Namespace: "entity", ID: "team/alice"}}, CreatedAt: now, UpdatedAt: now},
		"o-bbb": {ID: "o-bbb", Mentions: []uri.URI{{Scheme: "ctxt", Namespace: "entity", ID: "team/bob"}}, CreatedAt: now, UpdatedAt: now},
	}
	cits := []citation.Citation{
		{IDs: []string{"o-aaa", "o-bbb"}},
	}
	citation.EnrichCitationsWithEntities(cits, objMap)
	assert.Contains(t, cits[0].Entities, "ctxt://entity/team/alice")
	assert.Contains(t, cits[0].Entities, "ctxt://entity/team/bob")
}

func TestEnrichCitationsWithEntities_MissingObject(t *testing.T) {
	objMap := map[string]*storage.KnowledgeObject{}
	cits := []citation.Citation{
		{IDs: []string{"o-missing"}},
	}
	// Should not panic for missing objects.
	citation.EnrichCitationsWithEntities(cits, objMap)
	assert.Empty(t, cits[0].Entities)
}

// ---------------------------------------------------------------------------
// ExtractCitationIDs (unique IDs from all citations)
// ---------------------------------------------------------------------------

func TestExtractCitationIDs(t *testing.T) {
	cits := []citation.Citation{
		{IDs: []string{"o-abc123"}},
		{IDs: []string{"o-def456", "o-abc123"}}, // duplicate
	}
	ids := citation.ExtractCitationIDs(cits)
	assert.Len(t, ids, 2)
	assert.Contains(t, ids, "o-abc123")
	assert.Contains(t, ids, "o-def456")
}

func TestExtractCitationIDs_Empty(t *testing.T) {
	ids := citation.ExtractCitationIDs(nil)
	assert.Empty(t, ids)
}

// ---------------------------------------------------------------------------
// CompositionContext
// ---------------------------------------------------------------------------

func TestCompositionContext_BuildObjectMap(t *testing.T) {
	now := time.Now()
	objs := []*storage.KnowledgeObject{
		{ID: "o-aaa", Type: "note", CreatedAt: now, UpdatedAt: now},
		{ID: "o-bbb", Type: "decision", CreatedAt: now, UpdatedAt: now},
	}
	ctx := citation.NewCompositionContext(objs)
	assert.Len(t, ctx.ObjectMap, 2)
	assert.Equal(t, "note", ctx.ObjectMap["o-aaa"].Type)
	assert.Equal(t, "decision", ctx.ObjectMap["o-bbb"].Type)
}

func TestCompositionContext_Objects(t *testing.T) {
	now := time.Now()
	objs := []*storage.KnowledgeObject{
		{ID: "o-aaa", Type: "note", CreatedAt: now, UpdatedAt: now},
	}
	ctx := citation.NewCompositionContext(objs)
	assert.Len(t, ctx.Objects, 1)
}

package markdownexport_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	uri "hop.top/cite/scheme"

	markdownexport "github.com/ideacrafterslabs/ctxt-plugin-markdown-export"
	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
)

func baseObject() pluginapi.KnowledgeObject {
	now := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	return pluginapi.KnowledgeObject{
		ID:        "obj_abc123",
		Type:      "url",
		Source:    "https://example.com/article",
		Summaries: []string{"A brief summary of the article."},
		Tags: []pluginapi.Tag{
			{Label: "go", Source: "llm"},
			{Label: "plugin system", Source: "llm"},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
}

func TestRender_Frontmatter(t *testing.T) {
	obj := baseObject()
	out, err := markdownexport.Render(obj)
	require.NoError(t, err)
	s := string(out)

	assert.Contains(t, s, "---\n")
	assert.Contains(t, s, "id: obj_abc123")
	assert.Contains(t, s, "type: url")
	assert.Contains(t, s, "tags:")
	assert.Contains(t, s, "  - go")
	assert.Contains(t, s, "  - plugin-system") // spaces → hyphens
	assert.Contains(t, s, "created: 2026-01-15T12:00:00Z")
	assert.Contains(t, s, `source: "https://example.com/article"`)
}

func TestRender_Title_FromMetadata(t *testing.T) {
	obj := baseObject()
	obj.Metadata = map[string]any{"title": "My Article"}
	out, err := markdownexport.Render(obj)
	require.NoError(t, err)
	assert.Contains(t, string(out), "# My Article")
}

func TestRender_Title_FromSummary(t *testing.T) {
	obj := baseObject()
	// No metadata title — falls back to first summary.
	out, err := markdownexport.Render(obj)
	require.NoError(t, err)
	assert.Contains(t, string(out), "# A brief summary of the article.")
}

func TestRender_Title_FallbackToID(t *testing.T) {
	obj := baseObject()
	obj.Summaries = nil
	out, err := markdownexport.Render(obj)
	require.NoError(t, err)
	assert.Contains(t, string(out), "# obj_abc123")
}

func TestRender_Mentions_Backlinks(t *testing.T) {
	obj := baseObject()
	u, _ := uri.Parse("@go.plugin-system")
	obj.Mentions = []uri.URI{*u}

	out, err := markdownexport.Render(obj)
	require.NoError(t, err)
	s := string(out)

	assert.Contains(t, s, "entities:")
	assert.Contains(t, s, "## Backlinks")
	assert.Contains(t, s, "[[")
}

func TestRender_Decisions(t *testing.T) {
	obj := baseObject()
	obj.Decisions = []pluginapi.Decision{
		{Title: "Use SQLite", Status: "accepted", Impact: "high"},
	}
	out, err := markdownexport.Render(obj)
	require.NoError(t, err)
	s := string(out)
	assert.Contains(t, s, "## Decisions")
	assert.Contains(t, s, "**Use SQLite**")
}

func TestRender_Tasks_Checkboxes(t *testing.T) {
	obj := baseObject()
	obj.Tasks = []pluginapi.Task{
		{Title: "Write tests", Status: "todo"},
		{Title: "Ship feature", Status: "done"},
	}
	out, err := markdownexport.Render(obj)
	require.NoError(t, err)
	s := string(out)
	assert.Contains(t, s, "- [ ] Write tests")
	assert.Contains(t, s, "- [x] Ship feature")
}

func TestRender_Sections(t *testing.T) {
	obj := baseObject()
	obj.Sections = []pluginapi.Section{
		{Title: "Intro", Content: "Intro body.", Order: 0},
	}
	out, err := markdownexport.Render(obj)
	require.NoError(t, err)
	s := string(out)
	assert.Contains(t, s, "## Intro")
	assert.Contains(t, s, "Intro body.")
}

func TestRender_NoExtraNewlines(t *testing.T) {
	obj := baseObject()
	out, err := markdownexport.Render(obj)
	require.NoError(t, err)
	// Should not have triple blank lines.
	assert.NotContains(t, string(out), "\n\n\n\n")
}

func TestRender_LongSummaryTruncated(t *testing.T) {
	obj := baseObject()
	obj.Summaries = []string{strings.Repeat("x", 100)}
	obj.Metadata = nil
	out, err := markdownexport.Render(obj)
	require.NoError(t, err)
	// Title heading should be ≤ 80 chars + "# " + newline.
	lines := strings.Split(string(out), "\n")
	for _, l := range lines {
		if strings.HasPrefix(l, "# ") {
			assert.LessOrEqual(t, len(l), 85, "title line too long: %q", l)
		}
	}
}

func TestRender_GraphCanonical_UsesProjection(t *testing.T) {
	// Graph-canonical KO: no flat Summaries/Sections — all content in Graph.
	now := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	obj := pluginapi.KnowledgeObject{
		ID:        "obj_graph_render",
		Type:      "text",
		CreatedAt: now,
		UpdatedAt: now,
		Graph: &pluginapi.ObjectGraph{
			Nodes: []pluginapi.GraphNode{
				{
					ID:       "obj_graph_render/summary/0",
					NodeType: pluginapi.NodeTypeSummary,
					Content:  "Graph-sourced summary body.",
					Order:    0,
				},
				{
					ID:       "obj_graph_render/section/0",
					NodeType: pluginapi.NodeTypeSection,
					Label:    "Details",
					Content:  "Section content here.",
					Order:    0,
				},
				{
					ID:       "obj_graph_render/tag/0",
					NodeType: pluginapi.NodeTypeTag,
					Label:    "graph-tag",
					Content:  "graph-tag",
					Order:    0,
				},
			},
		},
	}

	out, err := markdownexport.Render(obj)
	require.NoError(t, err)
	s := string(out)

	// Tags from graph nodes appear in frontmatter.
	assert.Contains(t, s, "tags:")
	assert.Contains(t, s, "  - graph-tag")

	// Section from graph appears in body.
	assert.Contains(t, s, "## Details")
	assert.Contains(t, s, "Section content here.")

	// Title uses FTSBody when no metadata title and Graph is present.
	assert.Contains(t, s, "Graph-sourced summary body.")
}

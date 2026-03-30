package panes_test

import (
	"testing"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/tui"
	"github.com/ideacrafterslabs/ctxt/internal/tui/panes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hop.top/uri"
)

func TestPreviewPaneObjectLoaded(t *testing.T) {
	theme := tui.DefaultTheme()
	pp := panes.NewPreviewPane(theme)

	// TextContent is the projection-canonical body field.
	obj := &storage.KnowledgeObject{
		ID:          "obj1",
		Type:        "text",
		TextContent: "This is a summary.",
		Tags:        []storage.Tag{{Label: "go", Weight: 0.9}},
		Mentions:    []uri.URI{{Scheme: "ctxt", Space: "entity", ID: "infra/db"}},
		Sections:    []storage.Section{{Title: "Intro", Content: "Hello"}},
	}

	updated, cmd := pp.Update(tui.ObjectLoadedMsg{Object: obj})
	assert.Nil(t, cmd)
	require.NotNil(t, updated)
	view := updated.View(80, 24)
	assert.Contains(t, view, "This is a summary.")
}

// TestPreviewPaneObjectLoadedLegacySummaries verifies the Summaries fallback
// for KOs that predate TextContent/graph migration.
func TestPreviewPaneObjectLoadedLegacySummaries(t *testing.T) {
	theme := tui.DefaultTheme()
	pp := panes.NewPreviewPane(theme)

	obj := &storage.KnowledgeObject{
		ID:        "obj-legacy",
		Type:      "text",
		Summaries: []string{"Legacy summary text."},
	}

	updated, cmd := pp.Update(tui.ObjectLoadedMsg{Object: obj})
	assert.Nil(t, cmd)
	require.NotNil(t, updated)
	view := updated.View(80, 24)
	assert.Contains(t, view, "Legacy summary text.")
}

func TestPreviewPaneSubviewToggle(t *testing.T) {
	theme := tui.DefaultTheme()
	pp := panes.NewPreviewPane(theme)

	obj := &storage.KnowledgeObject{
		ID:        "obj2",
		Summaries: []string{"Summary text"},
		Tags:      []storage.Tag{{Label: "design", Weight: 0.8}},
		Mentions: []uri.URI{{Scheme: "ctxt", Space: "entity", ID: "ui/form"}},
		Sections:  []storage.Section{{Title: "Sec1", Content: "Section content"}},
	}
	pp.SetObject(obj)
	pp.Focus() // Must be focused for key handling

	// Toggle to tags view with 't'
	updated, _ := pp.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	view := updated.View(80, 24)
	assert.Contains(t, view, "design")

	// Toggle to mentions view with 'm'
	updated, _ = updated.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
	view = updated.View(80, 24)
	assert.Contains(t, view, "ctxt://entity/ui/form")

	// Toggle to sections view with 's'
	updated, _ = updated.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	view = updated.View(80, 24)
	assert.Contains(t, view, "Sec1")
}

func TestPreviewPaneViewNonEmpty(t *testing.T) {
	_ = viewport.New(80, 24) // ensure import used
	theme := tui.DefaultTheme()
	pp := panes.NewPreviewPane(theme)
	view := pp.View(80, 24)
	assert.NotEmpty(t, view)
}

// TestPreviewPaneGraphSectionsFromNodes verifies DocumentProjection uses graph
// section nodes instead of flat Sections when Graph is present.
func TestPreviewPaneGraphSectionsFromNodes(t *testing.T) {
	theme := tui.DefaultTheme()
	pp := panes.NewPreviewPane(theme)

	graph := &storage.ObjectGraph{
		Nodes: []storage.GraphNode{
			{
				ID:       "obj3/section/0",
				NodeType: "section",
				Label:    "Graph Section",
				Content:  "Content from graph node",
				Order:    0,
			},
		},
	}
	obj := &storage.KnowledgeObject{
		ID:    "obj3",
		Type:  "text",
		Graph: graph,
		// Flat sections should NOT appear; graph wins.
		Sections: []storage.Section{{Title: "Flat Section", Content: "Flat content"}},
	}
	pp.SetObject(obj)
	pp.Focus()

	// Switch to sections subview.
	updated, _ := pp.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	view := updated.View(80, 24)
	assert.Contains(t, view, "Graph Section", "graph section title expected")
	assert.Contains(t, view, "Content from graph node", "graph section content expected")
	assert.NotContains(t, view, "Flat Section", "flat section must not appear when graph present")
}

// TestPreviewPaneGraphDecisionNodes verifies decision nodes surface in the
// sections subview as a grouped badge list.
func TestPreviewPaneGraphDecisionNodes(t *testing.T) {
	theme := tui.DefaultTheme()
	pp := panes.NewPreviewPane(theme)

	graph := &storage.ObjectGraph{
		Nodes: []storage.GraphNode{
			{
				ID:       "obj4/decision/0",
				NodeType: "decision",
				Label:    "Use projection helpers",
				Content:  "approved",
				Order:    0,
			},
		},
	}
	obj := &storage.KnowledgeObject{
		ID:    "obj4",
		Type:  "text",
		Graph: graph,
	}
	pp.SetObject(obj)
	pp.Focus()

	updated, _ := pp.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	view := updated.View(80, 24)
	assert.Contains(t, view, "decisions", "decisions header expected")
	assert.Contains(t, view, "Use projection helpers", "decision label expected")
}

// TestPreviewPaneIndexProjectionTags verifies tags subview uses IndexProjection
// graph path when a graph is present.
func TestPreviewPaneIndexProjectionTags(t *testing.T) {
	theme := tui.DefaultTheme()
	pp := panes.NewPreviewPane(theme)

	graph := &storage.ObjectGraph{
		Nodes: []storage.GraphNode{
			{ID: "obj5/tag/0", NodeType: "tag", Label: "graph-tag"},
		},
	}
	obj := &storage.KnowledgeObject{
		ID:    "obj5",
		Type:  "text",
		Graph: graph,
		// Flat tags should NOT appear; graph wins.
		Tags: []storage.Tag{{Label: "flat-tag", Weight: 1.0}},
	}
	pp.SetObject(obj)
	pp.Focus()

	updated, _ := pp.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	view := updated.View(80, 24)
	assert.Contains(t, view, "graph-tag", "graph tag expected")
	assert.NotContains(t, view, "flat-tag", "flat tag must not appear when graph present")
}

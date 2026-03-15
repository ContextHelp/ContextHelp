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

	obj := &storage.KnowledgeObject{
		ID:        "obj1",
		Type:      "text",
		Summaries: []string{"This is a summary."},
		Tags:      []storage.Tag{{Label: "go", Weight: 0.9}},
		MentionURIs: []uri.URI{{Scheme: "ctxt", Space: "entity", ID: "infra/db"}},
		Sections:  []storage.Section{{Title: "Intro", Content: "Hello"}},
	}

	updated, cmd := pp.Update(tui.ObjectLoadedMsg{Object: obj})
	assert.Nil(t, cmd)
	require.NotNil(t, updated)
	view := updated.View(80, 24)
	assert.Contains(t, view, "This is a summary.")
}

func TestPreviewPaneSubviewToggle(t *testing.T) {
	theme := tui.DefaultTheme()
	pp := panes.NewPreviewPane(theme)

	obj := &storage.KnowledgeObject{
		ID:        "obj2",
		Summaries: []string{"Summary text"},
		Tags:      []storage.Tag{{Label: "design", Weight: 0.8}},
		MentionURIs: []uri.URI{{Scheme: "ctxt", Space: "entity", ID: "ui/form"}},
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

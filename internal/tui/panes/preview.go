package panes

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/tui/types"
)

type subview string

const (
	subviewSummary  subview = "summary"
	subviewTags     subview = "tags"
	subviewMentions subview = "mentions"
	subviewSections subview = "sections"
)

// PreviewPane displays the currently selected KnowledgeObject.
type PreviewPane struct {
	viewport viewport.Model
	object   *storage.KnowledgeObject
	subview  subview
	focused  bool
	theme    types.Theme
}

// NewPreviewPane constructs a ready-to-use PreviewPane.
func NewPreviewPane(theme types.Theme) *PreviewPane {
	vp := viewport.New(80, 20)
	vp.SetContent("No object selected. Use the search pane to find knowledge objects.")
	return &PreviewPane{
		viewport: vp,
		subview:  subviewSummary,
		theme:    theme,
	}
}

// SetObject sets the object to preview.
func (p *PreviewPane) SetObject(obj *storage.KnowledgeObject) {
	p.object = obj
	p.viewport.SetContent(p.renderSubview())
	p.viewport.GotoTop()
}

// Focus implements Pane.
func (p *PreviewPane) Focus() { p.focused = true }

// Blur implements Pane.
func (p *PreviewPane) Blur() { p.focused = false }

// IsFocused implements Pane.
func (p *PreviewPane) IsFocused() bool { return p.focused }

// Update implements Pane.
func (p *PreviewPane) Update(msg tea.Msg) (Pane, tea.Cmd) {
	switch msg := msg.(type) {
	case types.ObjectLoadedMsg:
		p.SetObject(msg.Object)
		return p, nil

	case tea.KeyMsg:
		if !p.focused {
			break
		}
		switch msg.String() {
		case "d":
			p.subview = subviewSummary
			p.viewport.SetContent(p.renderSubview())
			return p, nil
		case "t":
			p.subview = subviewTags
			p.viewport.SetContent(p.renderSubview())
			return p, nil
		case "m":
			p.subview = subviewMentions
			p.viewport.SetContent(p.renderSubview())
			return p, nil
		case "s":
			p.subview = subviewSections
			p.viewport.SetContent(p.renderSubview())
			return p, nil
		}
	}

	var cmd tea.Cmd
	p.viewport, cmd = p.viewport.Update(msg)
	return p, cmd
}

// View implements Pane.
func (p *PreviewPane) View(width, height int) string {
	borderStyle := p.theme.Blurred
	if p.focused {
		borderStyle = p.theme.Focused
	}

	innerW := width - 4
	innerH := height - 4
	if innerW < 1 {
		innerW = 1
	}
	if innerH < 1 {
		innerH = 1
	}

	p.viewport.Width = innerW
	p.viewport.Height = innerH

	subviewLabel := p.theme.Muted.Render(
		fmt.Sprintf(" [d]summary [t]tags [m]mentions [s]sections | %s", string(p.subview)),
	)

	content := lipgloss.JoinVertical(
		lipgloss.Left,
		subviewLabel,
		p.viewport.View(),
	)

	return borderStyle.Width(width - 2).Height(height - 2).Render(content)
}

func (p *PreviewPane) renderSubview() string {
	if p.object == nil {
		return "No object selected."
	}
	switch p.subview {
	case subviewSummary:
		return p.renderSummary()
	case subviewTags:
		return p.renderTags()
	case subviewMentions:
		return p.renderMentions()
	case subviewSections:
		return p.renderSections()
	default:
		return p.renderSummary()
	}
}

func (p *PreviewPane) renderSummary() string {
	var b strings.Builder
	fmt.Fprintf(&b, "ID: %s\nType: %s\nPipeline: %s\nSource: %s\n\n",
		p.object.ID, p.object.Type, p.object.Pipeline, p.object.Source)
	if len(p.object.Summaries) > 0 {
		b.WriteString(p.object.Summaries[0])
	} else {
		b.WriteString("(no summary)")
	}
	return b.String()
}

func (p *PreviewPane) renderTags() string {
	if len(p.object.Tags) == 0 {
		return "(no tags)"
	}
	var b strings.Builder
	for _, tag := range p.object.Tags {
		fmt.Fprintf(&b, "  %s (%.2f)\n", tag.Label, tag.Weight)
	}
	return b.String()
}

func (p *PreviewPane) renderMentions() string {
	if len(p.object.Mentions) == 0 {
		return "(no mentions)"
	}
	return strings.Join(p.object.Mentions, "\n")
}

func (p *PreviewPane) renderSections() string {
	if len(p.object.Sections) == 0 {
		return "(no sections)"
	}
	var b strings.Builder
	for _, sec := range p.object.Sections {
		fmt.Fprintf(&b, "## %s\n%s\n\n", sec.Title, sec.Content)
	}
	return b.String()
}

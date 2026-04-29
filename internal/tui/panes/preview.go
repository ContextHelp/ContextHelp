package panes

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/ideacrafterslabs/ctxt/internal/projection"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/tui/types"
	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
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
	vp := viewport.New()
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

	p.viewport.SetWidth(innerW)
	p.viewport.SetHeight(innerH)

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

// renderSummary uses DocumentProjection for body/title; falls back to flat Summaries then "(no summary)".
func (p *PreviewPane) renderSummary() string {
	dp := projection.ProjectDocument(p.object)
	var b strings.Builder
	fmt.Fprintf(&b, "ID: %s\nType: %s\nPipeline: %s\nSource: %s\n\n",
		p.object.ID, p.object.Type, p.object.Pipeline, p.object.Source)
	switch {
	case dp.Body != "":
		b.WriteString(dp.Body)
	case len(dp.Sections) > 0:
		b.WriteString(dp.Sections[0].Content)
	case len(p.object.Summaries) > 0:
		// Legacy flat-field fallback: KOs without graph or TextContent.
		b.WriteString(p.object.Summaries[0])
	default:
		b.WriteString("(no summary)")
	}
	return b.String()
}

// renderTags uses IndexProjection for tag list.
func (p *PreviewPane) renderTags() string {
	ip := projection.ProjectIndex(p.object)
	if len(ip.Tags) == 0 {
		return "(no tags)"
	}
	var b strings.Builder
	for _, tag := range ip.Tags {
		fmt.Fprintf(&b, "  %s (%.2f)\n", tag.Label, tag.Weight)
	}
	return b.String()
}

// renderMentions uses IndexProjection for mention list.
func (p *PreviewPane) renderMentions() string {
	ip := projection.ProjectIndex(p.object)
	if len(ip.Mentions) == 0 {
		return "(no mentions)"
	}
	return strings.Join(ip.Mentions, "\n")
}

// renderSections uses DocumentProjection for section list.
// Decision nodes from the graph are grouped and rendered as a badge list above sections.
func (p *PreviewPane) renderSections() string {
	dp := projection.ProjectDocument(p.object)
	var b strings.Builder

	// Graph-aware: surface decision nodes as a grouped block.
	decisions := graphNodesOfType(p.object, pluginapi.NodeTypeDecision)
	if len(decisions) > 0 {
		b.WriteString("── decisions ──\n")
		for _, n := range decisions {
			label := n.Label
			if label == "" {
				label = n.Content
			}
			fmt.Fprintf(&b, "  [%s] %s\n", n.Content, label)
		}
		b.WriteString("\n")
	}

	if len(dp.Sections) == 0 {
		if b.Len() == 0 {
			return "(no sections)"
		}
		return b.String()
	}
	for _, sec := range dp.Sections {
		fmt.Fprintf(&b, "## %s\n%s\n\n", sec.Title, sec.Content)
	}
	return b.String()
}

// graphNodesOfType returns all nodes of the given type from ko.Graph, or nil.
func graphNodesOfType(ko *storage.KnowledgeObject, nodeType string) []pluginapi.GraphNode {
	if ko == nil || ko.Graph == nil {
		return nil
	}
	var out []pluginapi.GraphNode
	for _, n := range ko.Graph.Nodes {
		if n.NodeType == nodeType {
			out = append(out, n)
		}
	}
	return out
}

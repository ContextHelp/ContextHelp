package panes

import (
	"context"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/ideacrafterslabs/ctxt/internal/projection"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/tui/types"
)

const debounceDelay = 250 * time.Millisecond

// resultItem adapts *storage.KnowledgeObject to the bubbles/list.Item interface.
type resultItem struct {
	obj *storage.KnowledgeObject
}

func (r resultItem) Title() string {
	dp := projection.ProjectDocument(r.obj)
	switch {
	case dp.Title != "":
		return dp.Title
	case dp.Body != "":
		// Truncate body to a single line for the list title.
		line := dp.Body
		if nl := strings.IndexByte(line, '\n'); nl >= 0 {
			line = line[:nl]
		}
		if len(line) > 80 {
			line = line[:80] + "…"
		}
		return line
	case len(dp.Sections) > 0 && dp.Sections[0].Title != "":
		return dp.Sections[0].Title
	default:
		return r.obj.ID
	}
}

func (r resultItem) Description() string {
	ip := projection.ProjectIndex(r.obj)
	var parts []string
	if r.obj.Type != "" {
		parts = append(parts, r.obj.Type)
	}
	// Surface up to 3 tags as preview badges.
	for i, tag := range ip.Tags {
		if i >= 3 {
			break
		}
		parts = append(parts, "#"+tag.Label)
	}
	if len(parts) == 0 {
		return r.obj.Pipeline
	}
	return strings.Join(parts, " · ")
}

func (r resultItem) FilterValue() string { return r.Title() }

// SearchPane holds the search input and results list.
type SearchPane struct {
	input    textinput.Model
	list     list.Model
	adapter  types.ServiceAdapter
	theme    types.Theme
	focused  bool
	results  []*storage.KnowledgeObject
	debounce *time.Timer
}

// NewSearchPane constructs a ready-to-use SearchPane.
func NewSearchPane(adapter types.ServiceAdapter, theme types.Theme) *SearchPane {
	ti := textinput.New()
	ti.Placeholder = "Search knowledge... (type to search)"
	ti.CharLimit = 256

	delegate := list.NewDefaultDelegate()
	l := list.New(nil, delegate, 0, 0)
	l.Title = "Results"
	l.SetShowStatusBar(true)
	l.SetFilteringEnabled(false)

	return &SearchPane{
		input:   ti,
		list:    l,
		adapter: adapter,
		theme:   theme,
	}
}

// ResultCount returns the number of results currently displayed.
func (s *SearchPane) ResultCount() int { return len(s.results) }

// SelectedObject returns the KnowledgeObject under the list cursor, or nil.
func (s *SearchPane) SelectedObject() *storage.KnowledgeObject {
	item, ok := s.list.SelectedItem().(resultItem)
	if !ok {
		return nil
	}
	return item.obj
}

// Focus implements Pane.
func (s *SearchPane) Focus() {
	s.focused = true
	s.input.Focus()
}

// Blur implements Pane.
func (s *SearchPane) Blur() {
	s.focused = false
	s.input.Blur()
}

// IsFocused implements Pane.
func (s *SearchPane) IsFocused() bool { return s.focused }

// localSearchCmd returns a tea.Cmd for the given query.
func localSearchCmd(adapter types.ServiceAdapter, query string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		results, err := adapter.Search(ctx, query, 50)
		if err != nil {
			return types.ErrorMsg{Err: err}
		}
		return types.SearchResultsMsg{Results: results}
	}
}

// Update implements Pane.
func (s *SearchPane) Update(msg tea.Msg) (Pane, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case types.SearchResultsMsg:
		s.results = msg.Results
		items := make([]list.Item, len(msg.Results))
		for i, obj := range msg.Results {
			items[i] = resultItem{obj: obj}
		}
		s.list.SetItems(items)
		return s, nil

	case tea.KeyMsg:
		if s.focused {
			var tiCmd tea.Cmd
			s.input, tiCmd = s.input.Update(msg)
			cmds = append(cmds, tiCmd)

			if s.debounce != nil {
				s.debounce.Stop()
			}
			query := s.input.Value()
			if query != "" {
				s.debounce = time.AfterFunc(debounceDelay, func() {})
				cmds = append(cmds, localSearchCmd(s.adapter, query))
			}
		}

	default:
		var listCmd tea.Cmd
		s.list, listCmd = s.list.Update(msg)
		cmds = append(cmds, listCmd)
	}

	return s, tea.Batch(cmds...)
}

// View implements Pane.
func (s *SearchPane) View(width, height int) string {
	borderStyle := s.theme.Blurred
	if s.focused {
		borderStyle = s.theme.Focused
	}

	innerW := width - 4
	innerH := height - 4
	if innerW < 1 {
		innerW = 1
	}
	if innerH < 3 {
		innerH = 3
	}

	s.input.Width = innerW
	s.list.SetWidth(innerW)
	s.list.SetHeight(innerH - 2)

	content := lipgloss.JoinVertical(
		lipgloss.Left,
		s.input.View(),
		s.list.View(),
	)

	return borderStyle.Width(width - 2).Height(height - 2).Render(content)
}

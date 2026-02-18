# TUI (Terminal User Interface)

**Version:** 0.1.0

This document describes the **`ctxt` TUI** — an interactive, keyboard-driven terminal interface built with [Charmbracelet Bubbletea](https://github.com/charmbracelet/bubbletea).

The TUI provides a rich, visual interface for exploring knowledge objects, navigating the knowledge graph, and composing outputs without leaving the terminal.

**Status:** Planned for Phase 9 (Proof of Platform)

---

## Design Philosophy

The `ctxt` TUI follows these principles:

**Keyboard-First**
- Every action accessible via keyboard shortcuts
- Mouse support optional and supplementary
- Vim-inspired navigation where appropriate
- Context-sensitive keybindings

**Information-Dense Yet Readable**
- Split-pane layouts maximize screen real estate
- Smart truncation and ellipsis for long content
- Visual hierarchy through lipgloss styling
- Graceful degradation on small terminals

**Responsive and Performant**
- Lazy loading for large result sets
- Efficient rendering with minimal redraws
- Background operations don't block UI
- Smooth transitions and feedback

**Accessible by Default**
- High contrast mode
- Screen reader compatibility (where terminal supports)
- Configurable color schemes
- No essential information conveyed by color alone

---

## Architecture (Bubbletea Model-View-Update)

The TUI follows the **Elm Architecture** pattern via Bubbletea:

```
┌─────────────────────────────────────────┐
│              User Input                 │
│           (keyboard/mouse)              │
└─────────────┬───────────────────────────┘
              │
              ▼
┌─────────────────────────────────────────┐
│         Update(msg) → Model             │
│  • Handle keyboard events               │
│  • Process background job results       │
│  • Update application state             │
│  • Return new model + commands          │
└─────────────┬───────────────────────────┘
              │
              ▼
┌─────────────────────────────────────────┐
│          View(model) → String           │
│  • Render current state                 │
│  • Apply lipgloss styles                │
│  • Compose pane layouts                 │
│  • Return ANSI-formatted output         │
└─────────────────────────────────────────┘
```

### Core Types

```go
// Main application model
type Model struct {
    // UI State
    activePane     PaneID
    width, height  int

    // Panes
    searchPane     *SearchPane
    previewPane    *PreviewPane
    graphPane      *GraphPane

    // Application State
    query          string
    results        []Object
    selectedResult int
    graph          *Graph

    // Background Operations
    jobsInFlight   map[string]JobHandle

    // Bubbles Components
    searchInput    textinput.Model
    resultsList    list.Model
    helpMenu       help.Model
}

// Message types (events)
type Msg interface{}

type KeyMsg     tea.KeyMsg
type SearchMsg  struct { Results []Object }
type GraphMsg   struct { Graph *Graph }
type JobMsg     struct { JobID string, Status JobStatus }
type ResizeMsg  struct { Width, Height int }
```

### Update Function Pattern

```go
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch msg := msg.(type) {

    case tea.KeyMsg:
        return m.handleKeyPress(msg)

    case SearchMsg:
        m.results = msg.Results
        m.selectedResult = 0
        return m, nil

    case GraphMsg:
        m.graph = msg.Graph
        return m, nil

    case ResizeMsg:
        m.width = msg.Width
        m.height = msg.Height
        return m.relayout(), nil
    }

    return m, nil
}
```

### View Function Pattern

```go
func (m Model) View() string {
    if m.width == 0 {
        return "Loading..."
    }

    // Compute pane dimensions
    searchWidth := m.width / 3
    previewWidth := m.width - searchWidth

    // Render panes
    searchView := m.renderSearchPane(searchWidth, m.height)
    previewView := m.renderPreviewPane(previewWidth, m.height)

    // Compose layout
    return lipgloss.JoinHorizontal(
        lipgloss.Top,
        searchView,
        previewView,
    )
}
```

### Command Pattern (Side Effects)

```go
// Commands represent side effects (IO, async operations)
func searchCommand(query string) tea.Cmd {
    return func() tea.Msg {
        results, err := executeSearch(query)
        if err != nil {
            return ErrorMsg{err}
        }
        return SearchMsg{Results: results}
    }
}

func loadGraphCommand(objectID string) tea.Cmd {
    return func() tea.Msg {
        graph, err := loadGraph(objectID)
        if err != nil {
            return ErrorMsg{err}
        }
        return GraphMsg{Graph: graph}
    }
}
```

---

## Component Catalog

The TUI is built from reusable Bubbletea components (from `charmbracelet/bubbles` and custom):

### Input Components

**Search Input** (`textinput.Model`)
```go
searchInput := textinput.New()
searchInput.Placeholder = "Search knowledge..."
searchInput.Focus()
searchInput.CharLimit = 256
searchInput.Width = 40
```

**Multiline Editor** (custom or `textarea.Model`)
```go
editor := textarea.New()
editor.Placeholder = "Capture content here..."
editor.ShowLineNumbers = false
editor.CharLimit = 10000
```

### List Components

**Results List** (`list.Model`)
```go
type ResultItem struct {
    object Object
}

func (i ResultItem) Title() string       { return i.object.Summary }
func (i ResultItem) Description() string { return formatMetadata(i.object) }
func (i ResultItem) FilterValue() string { return i.object.Summary }

resultsList := list.New(items, list.NewDefaultDelegate(), width, height)
resultsList.Title = "Search Results"
resultsList.SetShowStatusBar(true)
resultsList.SetFilteringEnabled(true)
```

**Tags List** (custom `bubbles/list`)
```go
// Displays tags with weights
type TagItem struct {
    label  string
    weight float64
}

// Renders as: "design (0.85) • ux (0.72) • ui (0.68)"
```

**Mentions List** (custom)
```go
// Displays @entity.slug mentions
type MentionItem struct {
    slug   string
    entity *Entity
}

// Renders as: "@ui.best-practice → UI Best Practice"
```

### Table Components

**Object Details Table** (`table.Model`)
```go
columns := []table.Column{
    {Title: "Field", Width: 20},
    {Title: "Value", Width: 60},
}

rows := []table.Row{
    {"ID", object.ID},
    {"Type", object.Type},
    {"Created", object.CreatedAt.Format(time.RFC3339)},
    {"Pipeline", object.Pipeline},
    {"Source", object.Source},
}

t := table.New(
    table.WithColumns(columns),
    table.WithRows(rows),
    table.WithFocused(false),
)
```

**Job Status Table** (custom)
```go
// Displays active jobs with progress indicators
columns := []table.Column{
    {Title: "Job ID", Width: 10},
    {Title: "Type", Width: 15},
    {Title: "Status", Width: 12},
    {Title: "Progress", Width: 30},
}
```

### Graph Components

**Entity Graph Visualization** (custom ASCII graph)
```go
// Renders knowledge graph using box-drawing characters
// Example output:
//
//   [Object A]───mentions───>[Entity: @ui.form]
//        │                         │
//        │                    mentions
//        │                         │
//        └─────related──────>[Object B]
```

**Graph Navigation Tree** (custom)
```go
// Hierarchical tree view of graph relationships
// Example output:
//
// ▼ [Object: Authentication Flow]
//   ├─ mentions @stripe.api (3 connections)
//   ├─ mentions @ui.form (5 connections)
//   └─ related to:
//       ├─ "Error Handling Patterns"
//       └─ "Checkout Flow Design"
```

### Status Components

**Progress Spinner** (`spinner.Model`)
```go
s := spinner.New()
s.Spinner = spinner.Dot
s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))
```

**Progress Bar** (`progress.Model`)
```go
prog := progress.New(progress.WithDefaultGradient())
prog.Width = 40
```

**Status Bar** (custom)
```go
// Bottom bar showing current state
// Format: "[Profile: engineer] | 142 results | Job: 3 running | <help> for commands"
```

### Help Components

**Help Menu** (`help.Model`)
```go
type keyMap struct {
    Search  key.Binding
    Open    key.Binding
    Graph   key.Binding
    Quit    key.Binding
}

func (k keyMap) ShortHelp() []key.Binding {
    return []key.Binding{k.Search, k.Open, k.Quit}
}

func (k keyMap) FullHelp() [][]key.Binding {
    return [][]key.Binding{
        {k.Search, k.Open, k.Graph},
        {k.Quit},
    }
}
```

---

## Keyboard Navigation Map

### Global Keybindings

| Key | Action | Context |
|-----|--------|---------|
| `?` | Toggle help | Always |
| `q` | Quit application | Always |
| `Ctrl+C` | Force quit | Always |
| `/` | Focus search input | Always |
| `Esc` | Clear focus / Go back | Context-dependent |
| `Tab` | Next pane | Multi-pane views |
| `Shift+Tab` | Previous pane | Multi-pane views |
| `Ctrl+R` | Refresh view | Always |

### Search Pane

| Key | Action |
|-----|--------|
| `Enter` | Execute search |
| `↑` / `k` | Previous result |
| `↓` / `j` | Next result |
| `Ctrl+D` | Page down |
| `Ctrl+U` | Page up |
| `g` | Go to top |
| `G` | Go to bottom |
| `Space` | Preview selected |
| `o` / `Enter` | Open selected in preview pane |

### Preview Pane

| Key | Action |
|-----|--------|
| `↑` / `k` | Scroll up |
| `↓` / `j` | Scroll down |
| `Ctrl+D` | Page down |
| `Ctrl+U` | Page up |
| `g` | Go to top |
| `G` | Go to bottom |
| `e` | Open in editor |
| `y` | Copy to clipboard |
| `d` | Show details (metadata) |
| `t` | Show tags |
| `m` | Show mentions |
| `s` | Show sections |

### Graph Pane

| Key | Action |
|-----|--------|
| `↑` / `k` | Navigate up |
| `↓` / `j` | Navigate down |
| `←` / `h` | Collapse node |
| `→` / `l` | Expand node |
| `Enter` | Load selected node |
| `b` | Back to previous node |
| `r` | Show related items |
| `e` | Show entities only |
| `o` | Show objects only |

### Composition Mode

| Key | Action |
|-----|--------|
| `Ctrl+N` | New composition |
| `Ctrl+S` | Save composition |
| `Ctrl+X` | Exit composition mode |
| `Ctrl+P` | Preview output |
| `Ctrl+E` | Edit template |
| `Tab` | Next template field |
| `Shift+Tab` | Previous template field |

### Job Management View

| Key | Action |
|-----|--------|
| `j` / `↓` | Next job |
| `k` / `↑` | Previous job |
| `r` | Retry failed job |
| `x` | Cancel running job |
| `l` | Show job log |
| `d` | Delete completed job |
| `a` | Retry all failed jobs |

---

## State Management Patterns

### Pane State Management

Each pane maintains its own state and communicates via messages:

```go
// Pane interface
type Pane interface {
    Update(msg tea.Msg) (Pane, tea.Cmd)
    View(width, height int) string
    Focus()
    Blur()
}

// SearchPane state
type SearchPane struct {
    focused      bool
    input        textinput.Model
    results      list.Model
    loading      bool
    lastQuery    string
}

// PreviewPane state
type PreviewPane struct {
    focused      bool
    object       *Object
    viewport     viewport.Model
    showMetadata bool
    showGraph    bool
}
```

### Focus Management

```go
type PaneID int

const (
    SearchPaneID PaneID = iota
    PreviewPaneID
    GraphPaneID
)

func (m *Model) focusPane(pane PaneID) tea.Cmd {
    // Blur all panes
    m.searchPane.Blur()
    m.previewPane.Blur()
    m.graphPane.Blur()

    // Focus target pane
    m.activePane = pane
    switch pane {
    case SearchPaneID:
        m.searchPane.Focus()
        return m.searchPane.input.Focus()
    case PreviewPaneID:
        m.previewPane.Focus()
        return nil
    case GraphPaneID:
        m.graphPane.Focus()
        return nil
    }
    return nil
}
```

### Background Job Tracking

```go
type JobTracker struct {
    jobs map[string]*JobStatus
    mu   sync.RWMutex
}

func (jt *JobTracker) Track(jobID string) tea.Cmd {
    return func() tea.Msg {
        // Poll job status
        ticker := time.NewTicker(500 * time.Millisecond)
        defer ticker.Stop()

        for range ticker.C {
            status, err := pollJobStatus(jobID)
            if err != nil {
                return JobErrorMsg{jobID, err}
            }

            if status.State == "completed" || status.State == "failed" {
                return JobCompletedMsg{jobID, status}
            }

            return JobProgressMsg{jobID, status}
        }
        return nil
    }
}
```

### Navigation History

```go
type History struct {
    stack   []HistoryEntry
    current int
}

type HistoryEntry struct {
    view     ViewType
    objectID string
    query    string
}

func (h *History) Push(entry HistoryEntry) {
    // Truncate forward history
    h.stack = h.stack[:h.current+1]
    h.stack = append(h.stack, entry)
    h.current = len(h.stack) - 1
}

func (h *History) Back() (HistoryEntry, bool) {
    if h.current <= 0 {
        return HistoryEntry{}, false
    }
    h.current--
    return h.stack[h.current], true
}

func (h *History) Forward() (HistoryEntry, bool) {
    if h.current >= len(h.stack)-1 {
        return HistoryEntry{}, false
    }
    h.current++
    return h.stack[h.current], true
}
```

### Cached Rendering

```go
type RenderCache struct {
    cache map[string]cachedView
    mu    sync.RWMutex
}

type cachedView struct {
    content   string
    timestamp time.Time
    hash      uint64
}

func (rc *RenderCache) Get(key string, model interface{}) (string, bool) {
    rc.mu.RLock()
    defer rc.mu.RUnlock()

    cached, ok := rc.cache[key]
    if !ok {
        return "", false
    }

    // Check if model hash matches (state unchanged)
    currentHash := hashModel(model)
    if currentHash != cached.hash {
        return "", false
    }

    return cached.content, true
}
```

---

## Styling System (Lipgloss)

### Style Definitions

```go
var (
    // Color Palette
    primaryColor   = lipgloss.Color("205") // Pink
    secondaryColor = lipgloss.Color("170") // Purple
    accentColor    = lipgloss.Color("86")  // Cyan
    mutedColor     = lipgloss.Color("241") // Gray
    errorColor     = lipgloss.Color("196") // Red
    successColor   = lipgloss.Color("42")  // Green

    // Text Styles
    titleStyle = lipgloss.NewStyle().
        Bold(true).
        Foreground(primaryColor).
        MarginBottom(1)

    subtitleStyle = lipgloss.NewStyle().
        Foreground(secondaryColor).
        Italic(true)

    mutedStyle = lipgloss.NewStyle().
        Foreground(mutedColor)

    highlightStyle = lipgloss.NewStyle().
        Foreground(accentColor).
        Bold(true)

    // Container Styles
    paneStyle = lipgloss.NewStyle().
        Border(lipgloss.RoundedBorder()).
        BorderForeground(mutedColor).
        Padding(1, 2)

    activePaneStyle = lipgloss.NewStyle().
        Border(lipgloss.RoundedBorder()).
        BorderForeground(primaryColor).
        Padding(1, 2)

    // Status Styles
    statusBarStyle = lipgloss.NewStyle().
        Background(mutedColor).
        Foreground(lipgloss.Color("255")).
        Padding(0, 1)

    errorStyle = lipgloss.NewStyle().
        Foreground(errorColor).
        Bold(true)

    successStyle = lipgloss.NewStyle().
        Foreground(successColor).
        Bold(true)
)
```

### Layout Patterns

**Horizontal Split**
```go
func renderSplitView(left, right string, width, height int) string {
    leftWidth := width / 2
    rightWidth := width - leftWidth

    leftPane := paneStyle.
        Width(leftWidth).
        Height(height).
        Render(left)

    rightPane := paneStyle.
        Width(rightWidth).
        Height(height).
        Render(right)

    return lipgloss.JoinHorizontal(lipgloss.Top, leftPane, rightPane)
}
```

**Vertical Split with Header/Footer**
```go
func renderPageLayout(header, body, footer string, width, height int) string {
    headerHeight := lipgloss.Height(header)
    footerHeight := lipgloss.Height(footer)
    bodyHeight := height - headerHeight - footerHeight - 2

    headerView := lipgloss.NewStyle().
        Width(width).
        Render(header)

    bodyView := lipgloss.NewStyle().
        Width(width).
        Height(bodyHeight).
        Render(body)

    footerView := statusBarStyle.
        Width(width).
        Render(footer)

    return lipgloss.JoinVertical(
        lipgloss.Left,
        headerView,
        bodyView,
        footerView,
    )
}
```

**Responsive Grid**
```go
func renderGrid(items []string, width, height int) string {
    cols := width / 40 // Assume 40 chars per item
    if cols < 1 {
        cols = 1
    }

    var rows []string
    for i := 0; i < len(items); i += cols {
        end := i + cols
        if end > len(items) {
            end = len(items)
        }

        row := lipgloss.JoinHorizontal(lipgloss.Top, items[i:end]...)
        rows = append(rows, row)
    }

    return lipgloss.JoinVertical(lipgloss.Left, rows...)
}
```

### Theme Support

```go
type Theme struct {
    Name              string
    Primary           lipgloss.Color
    Secondary         lipgloss.Color
    Accent            lipgloss.Color
    Muted             lipgloss.Color
    Error             lipgloss.Color
    Success           lipgloss.Color
    Background        lipgloss.Color
    Foreground        lipgloss.Color
}

var themes = map[string]Theme{
    "default": {
        Name:       "Default",
        Primary:    lipgloss.Color("205"),
        Secondary:  lipgloss.Color("170"),
        Accent:     lipgloss.Color("86"),
        Muted:      lipgloss.Color("241"),
        Error:      lipgloss.Color("196"),
        Success:    lipgloss.Color("42"),
        Background: lipgloss.Color("235"),
        Foreground: lipgloss.Color("255"),
    },
    "high-contrast": {
        Name:       "High Contrast",
        Primary:    lipgloss.Color("255"), // White
        Secondary:  lipgloss.Color("255"),
        Accent:     lipgloss.Color("255"),
        Muted:      lipgloss.Color("244"),
        Error:      lipgloss.Color("255"),
        Success:    lipgloss.Color("255"),
        Background: lipgloss.Color("0"),   // Black
        Foreground: lipgloss.Color("255"),
    },
    "solarized": {
        Name:       "Solarized Dark",
        Primary:    lipgloss.Color("33"),  // Blue
        Secondary:  lipgloss.Color("37"),  // Cyan
        Accent:     lipgloss.Color("64"),  // Green
        Muted:      lipgloss.Color("240"),
        Error:      lipgloss.Color("160"), // Red
        Success:    lipgloss.Color("64"),  // Green
        Background: lipgloss.Color("234"),
        Foreground: lipgloss.Color("244"),
    },
}

func applyTheme(t Theme) {
    primaryColor = t.Primary
    secondaryColor = t.Secondary
    accentColor = t.Accent
    mutedColor = t.Muted
    errorColor = t.Error
    successColor = t.Success

    // Rebuild styles with new colors
    titleStyle = titleStyle.Foreground(primaryColor)
    subtitleStyle = subtitleStyle.Foreground(secondaryColor)
    // ... etc
}
```

---

## Accessibility Considerations

### High Contrast Mode

```go
func enableHighContrast() {
    applyTheme(themes["high-contrast"])

    // Ensure borders use high contrast
    paneStyle = paneStyle.BorderForeground(lipgloss.Color("255"))
    activePaneStyle = activePaneStyle.BorderForeground(lipgloss.Color("255"))
}
```

### Screen Reader Support

The TUI provides text-based representations that work with terminal screen readers:

```go
// Always include text labels
func renderButton(label string, active bool) string {
    style := buttonStyle
    if active {
        style = activeButtonStyle
        label = "• " + label + " •" // Indicate active state
    }
    return style.Render(label)
}

// Avoid relying solely on color
func renderStatus(status string) string {
    switch status {
    case "completed":
        return successStyle.Render("✓ Completed")
    case "failed":
        return errorStyle.Render("✗ Failed")
    case "running":
        return highlightStyle.Render("⟳ Running")
    default:
        return mutedStyle.Render("○ " + status)
    }
}
```

### Keyboard-Only Navigation

All functionality accessible without mouse:

```go
// No essential features require mouse
// All actions have keyboard shortcuts
// Visual feedback for focused elements
func renderFocusIndicator(focused bool, content string) string {
    if focused {
        return activePaneStyle.Render(content)
    }
    return paneStyle.Render(content)
}
```

### Configurable UI Density

```go
type UIDensity int

const (
    Compact UIDensity = iota
    Normal
    Comfortable
)

func (m *Model) setDensity(d UIDensity) {
    switch d {
    case Compact:
        paneStyle = paneStyle.Padding(0, 1)
        m.resultsList.SetHeight(m.height - 4)
    case Normal:
        paneStyle = paneStyle.Padding(1, 2)
        m.resultsList.SetHeight(m.height - 6)
    case Comfortable:
        paneStyle = paneStyle.Padding(2, 3)
        m.resultsList.SetHeight(m.height - 8)
    }
}
```

### Terminal Compatibility

```go
func detectTerminalCapabilities() Capabilities {
    term := os.Getenv("TERM")
    colorterm := os.Getenv("COLORTERM")

    caps := Capabilities{
        Colors:      256,
        Unicode:     true,
        Mouse:       false,
        Clipboard:   false,
    }

    // Detect true color support
    if colorterm == "truecolor" || colorterm == "24bit" {
        caps.Colors = 16777216
    }

    // Detect unicode support
    if !strings.Contains(term, "xterm") {
        caps.Unicode = false
    }

    return caps
}

func adaptToCapabilities(caps Capabilities) {
    if caps.Colors < 256 {
        // Fall back to basic colors
        applyTheme(themes["high-contrast"])
    }

    if !caps.Unicode {
        // Use ASCII fallbacks
        lipgloss.SetDefaultRenderer(lipgloss.NewRenderer(
            lipgloss.WithAsciiOnly(true),
        ))
    }
}
```

### Focus Indicators

```go
// Clear visual feedback for focused elements
func renderListItem(item string, focused bool) string {
    if focused {
        return highlightStyle.Render("▶ " + item)
    }
    return "  " + item
}

// Announce focus changes for screen readers
func (m *Model) focusPane(pane PaneID) tea.Cmd {
    m.activePane = pane

    // Could integrate with terminal bell or status message
    m.statusMessage = fmt.Sprintf("Focused: %s", pane.Name())

    return m.announceFocus(pane)
}
```

---

## Implementation Phases

### Phase 1: Basic Shell
- Main model with single pane
- Search input and results list
- Basic keyboard navigation
- Simple lipgloss styling

### Phase 2: Split View
- Add preview pane
- Implement pane focus switching
- Add viewport for scrolling
- Improve layout responsiveness

### Phase 3: Graph View
- Add graph pane
- Implement ASCII graph rendering
- Navigation tree visualization
- Entity/object filtering

### Phase 4: Composition Mode
- Add composition interface
- Template selection
- Real-time preview
- Export functionality

### Phase 5: Polish
- Add themes
- Accessibility improvements
- Performance optimization
- User configuration

---

## Configuration

User configuration in `~/.config/ctxt/tui.yaml`:

```yaml
tui:
  theme: default  # default, high-contrast, solarized
  density: normal # compact, normal, comfortable

  keybindings:
    # Override default keybindings
    search: "/"
    quit: "q"
    help: "?"

  panes:
    default_layout: split  # split, tabbed, stacked
    search_width: 40       # percentage
    preview_width: 60

  performance:
    lazy_load: true
    cache_renders: true
    max_results: 1000

  accessibility:
    high_contrast: false
    screen_reader: false
    ascii_only: false
```

---

## Testing Strategy

See [testing.md](./testing.md) for complete TUI testing approach.

**Key Testing Areas:**
- Component unit tests
- Keyboard navigation flows
- Layout rendering at various sizes
- State management edge cases
- Accessibility compliance
- Performance under load

---

## References

**Charmbracelet Ecosystem:**
- [Bubbletea](https://github.com/charmbracelet/bubbletea) - TUI framework
- [Bubbles](https://github.com/charmbracelet/bubbles) - TUI components
- [Lip Gloss](https://github.com/charmbracelet/lipgloss) - Styling
- [Harmonica](https://github.com/charmbracelet/harmonica) - Animation
- [Glamour](https://github.com/charmbracelet/glamour) - Markdown rendering

**Related Documentation:**
- [interface-mappings.md](./interface-mappings.md) - Non-negotiable implementations
- [non-negotiables.md](./non-negotiables.md) - Design principles
- [../architecture.md](../architecture.md) - System architecture
- [api-cli.md](./api-cli.md) - CLI commands
- [testing.md](./testing.md) - Testing strategy
- [../dependencies.md](../dependencies.md) - TUI technology stack

---

**Status:** This document describes the planned TUI implementation. Design subject to refinement during Phase 9 development.

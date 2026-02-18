# Interface Mappings — Non-Negotiables Implementation

This document maps each non-negotiable principle to concrete implementations across different interfaces.

## Frictionless

**Principle:** Zero-resistance capture and retrieval. No rituals, no decisions, no "where does this go?" moments.

### CLI Implementation
- `ctxt add` accepts input from stdin, args, or prompts
- No required flags for basic capture
- Piping support: `echo "note" | ctxt add`
- Single command retrieval: `ctxt search <query>`

### TUI Implementation
- Instant search with `/` — no navigation required
- Single-key commands (no chords for common actions)
- No mode switching required
- Always ready to capture
- Zero-mode capture with `Ctrl+N`

**Zero-Mode Capture Example:**
```go
// No mode switching required - always ready to capture
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch msg := msg.(type) {
    case tea.KeyMsg:
        if msg.Type == tea.KeyRunes && msg.Runes[0] == '/' {
            // Instant search activation
            m.activePane = SearchPaneID
            m.searchInput.Focus()
            return m, nil
        }

        if msg.Type == tea.KeyCtrlN {
            // Instant capture mode
            return m, startCaptureMode()
        }
    }
    return m, nil
}
```

**Immediate Feedback:**
- Visual confirmation within 16ms (single frame)
- Status bar shows capture status instantly
- Background job indicator doesn't block UI
- Optimistic updates show result immediately

**No Classification Required:**
```go
// Accept any input without forcing categorization
func handleCapture(content string) tea.Cmd {
    // Enqueue job, let pipeline figure it out
    return enqueueCaptureJob(content)
}
```

**Single-Key Actions:**
| Key | Action | Frictionless Benefit |
|-----|--------|---------------------|
| `/` | Search | No need to navigate to search box |
| `o` | Open | Direct action from list |
| `Ctrl+N` | New capture | Muscle memory for "new" |
| `Esc` | Clear/Back | Universal escape hatch |
| `?` | Help | Discoverable without documentation |

---

## Accessible

**Principle:** Same brain, every interface. CLI, TUI, REPL, browser extension, web, mobile. Anywhere, instantly usable.

### CLI Implementation
- Works over SSH, no GUI required
- Minimal dependencies
- Scriptable and composable
- JSON output for machine readability: `ctxt search --json`
- Respects EDITOR, PAGER environment variables

### TUI Implementation
- Keyboard-first design — no mouse required
- High contrast mode available
- Screen reader compatible labels
- Responsive layouts for any terminal size
- Works on 80x24 minimum terminal
- ASCII fallback mode for limited terminals

**Keyboard-First Philosophy:**
- Every feature accessible via keyboard shortcuts
- No essential functionality requires mouse
- Vim-inspired navigation (hjkl) for familiarity
- Context-sensitive bindings reduce cognitive load

**Visual Accessibility:**
```go
// High contrast mode for low vision users
func enableHighContrast() {
    applyTheme(themes["high-contrast"])
    // White on black, no color-only information
}

// Screen reader compatibility
func renderWithLabels(content string, label string) string {
    // Always include text labels, not just icons
    return fmt.Sprintf("[%s] %s", label, content)
}

// Configurable UI density
func setDensity(d UIDensity) {
    // Compact, Normal, Comfortable modes
    // Accommodates different vision needs and preferences
}
```

**Terminal Compatibility:**
- Works on 80x24 minimum terminal size
- Graceful degradation to stacked view on small screens
- ASCII fallback mode for limited terminals
- No true color requirement (256-color compatible)

**Responsive Design:**
- Adapts to any terminal size automatically
- Split-pane, tabbed, or stacked layouts based on space
- Smart text wrapping and truncation
- Horizontal scrolling for wide content

---

## Formless

**Principle:** Accepts reality as-is. Any format, length, or mess. Nothing is rejected.

### CLI Implementation
- Accepts any text input without validation
- Handles multi-line input via heredoc or editor
- File input: `ctxt add < file.txt`
- No required structure or format

### TUI Implementation
- Multi-line text input
- File drag-and-drop (where terminal supports)
- Paste from clipboard
- No format validation at capture
- Handles any content length

---

## Polyglot

**Principle:** Speaks every language of knowledge and communication. Notes, tasks, code, media, research, and mixed Arabic/English/French stay usable and linkable.

### CLI Implementation
- UTF-8 support throughout
- Locale-aware sorting and search
- Language detection for metadata
- RTL-aware text wrapping

### TUI Implementation
- Unicode and RTL text support
- Language-aware rendering
- Mixed-script display without corruption
- Bidirectional text handling
- Proper character width calculation

---

## Trusted

**Principle:** Sharing is explicit and reversible. Publish, subscribe, or collaborate. Trust is earned, not assumed.

### CLI Implementation
- Explicit share commands: `ctxt share <id>`
- Unshare command: `ctxt unshare <id>`
- Privacy by default (local-first)
- Clear permission prompts for network operations

### TUI Implementation
- Clear permission prompts
- Explicit share actions
- Visual indicators for shared/private content
- Undo/revert operations
- Status shown in UI (lock icon for private, share icon for shared)

---

## Atomic

**Principle:** Notes are nodes, not essays. Small enough to link, reuse, remix, and reference. Built for composition.

### CLI Implementation
- Link creation: `ctxt link <source> <target>`
- Reference by ID or fuzzy name
- List backlinks: `ctxt backlinks <id>`
- Extract sections as new nodes

### TUI Implementation
- Section-based navigation
- Quick node splitting (`Ctrl+K` split at cursor)
- Visual graph view of connections
- Copy/reference shortcuts
- Inline link preview

---

## Discoverable

**Principle:** Retrieval is effortless under uncertainty. Fuzzy search, semantic search, filters, and recall without perfect memory.

### CLI Implementation
- Fuzzy search by default
- Filter flags: `--tag`, `--type`, `--date`
- Semantic search: `ctxt search --semantic <query>`
- Typo tolerance built-in

### TUI Implementation
- Incremental search-as-you-type
- Fuzzy matching with typo tolerance
- Entity-aware autocomplete
- Visual search result previews
- Search history navigation
- Filter UI with checkboxes

---

## Evergreen

**Principle:** Knowledge compounds instead of rotting. Notes get revisited, re-linked, refined, and updated as understanding grows.

### CLI Implementation
- Update command: `ctxt update <id>`
- History viewing: `ctxt history <id>`
- Diff between versions
- Automated suggestions for outdated content

### TUI Implementation
- Automatic resurfacing notifications
- Related item sidebar
- Backlink visualization
- Edit history navigation
- Visual diff view
- "Stale" indicator for old content

---

## Actionable

**Principle:** Insight must create movement. Every capture becomes a decision, next step, draft, task, or deliberate archive.

### CLI Implementation
- Task extraction: `ctxt extract-tasks <id>`
- Export formats: `--format md|html|json`
- Decision tagging: `ctxt tag <id> decision`
- Archive command: `ctxt archive <id>`

### TUI Implementation
- One-key composition triggers (`Ctrl+N` for brief)
- Inline task extraction
- Quick decision tagging (`t` key)
- Export to multiple formats (menu)
- Archive shortcut (`a` key)

---

## Accessibility Checklist (TUI)

The TUI must satisfy these accessibility requirements:

- [ ] All features accessible via keyboard
- [ ] High contrast mode available
- [ ] Text labels for all icons
- [ ] No essential information conveyed by color alone
- [ ] Focus indicators clearly visible
- [ ] Screen reader compatible structure
- [ ] Configurable UI density (compact/normal/comfortable)
- [ ] Works on 80x24 minimum terminal
- [ ] ASCII-only fallback mode
- [ ] Keyboard shortcut documentation always available
- [ ] Undo/redo for all destructive actions
- [ ] Status announcements for state changes
- [ ] Error messages clear and actionable

---

## See Also

- [non-negotiables.md](./non-negotiables.md) - Core principles
- [tui.md](./tui.md) - Complete TUI architecture and design rationale
- [api-cli.md](./api-cli.md) - CLI commands and API
- [testing.md](./testing.md) - Testing strategies
- [../design.md](../design.md) - Design patterns

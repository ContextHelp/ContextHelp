# WebUI (Web User Interface)

**Version:** 0.1.0

This document defines the **requirements, principles, and technical specifications** for the `ctxt` WebUI — a modern, accessible web interface for interacting with the knowledge management system.

**Status:** Requirements Definition (for future implementation)

---

## Core Requirements

### Functional Requirements

**Knowledge Management**
- Universal capture with persistent global search (always accessible)
- Browse, search, and filter knowledge objects
- View object details with full metadata, tags, and mentions
- Navigate entity graph visually
- Create and edit knowledge objects inline
- Composition interface for briefs, plans, and drafts
- Export and share functionality

**Real-Time Features**
- Live search results as you type
- Background job status indicators
- Optimistic UI updates for captures
- Progress tracking for long-running operations
- Notification system for resurfaced items

**Multi-User Collaboration** (future)
- Explicit sharing controls per object
- Privacy indicators (private/shared/public)
- Audit trail for sharing actions
- Collaborative editing with conflict resolution

---

## Non-Negotiables Implementation

These are the core principles from [non-negotiables.md](./non-negotiables.md) as they apply to the WebUI:

### Frictionless
- **Global capture**: `Cmd/Ctrl+K` opens quick capture from anywhere
- **Persistent search bar**: Always visible at top
- **No page reloads**: Single-page app architecture
- **Autosave**: No explicit "save" buttons, changes persist automatically
- **Instant preview**: See formatted output as you type
- **No required fields**: Accept any content without forcing structure
- **Quick actions**: Context menus and keyboard shortcuts for common tasks

### Accessible
- **WCAG 2.1 AA compliance** minimum (AAA for text)
- **Keyboard navigation**: All features accessible without mouse
- **Screen reader support**: ARIA landmarks, labels, and live regions
- **Responsive design**: Desktop, tablet, and mobile layouts
- **Dark mode**: System preference detection with manual toggle
- **High contrast option**: For low vision users
- **Focus indicators**: Clear visual feedback for focused elements
- **Skip links**: Navigate to main content areas
- **Resizable text**: Support up to 200% zoom without breaking layout

### Formless
- **Rich text editor** with markdown support
- **Drag-and-drop**: Files, images, text from any source
- **Paste handling**: Images, links, formatted text preserved
- **No validation at capture**: Accept anything, enrich later
- **Progressive enhancement**: Advanced features for capable browsers

### Polyglot
- **Full Unicode support** in all text fields
- **RTL text support**: Arabic, Hebrew layout adaptation
- **Mixed-direction text**: Bidirectional rendering
- **Language detection**: Automatic metadata tagging
- **Font fallback**: International character support
- **Translation UI**: View translated summaries and tags (when available)

### Trusted
- **Privacy by default**: All captures are private initially
- **Explicit sharing**: Clear "Share" button with confirmation
- **Visual privacy badges**: Lock icons for private, share icons for shared
- **One-click unshare**: Easy reversal of sharing
- **Audit log**: View sharing history per object
- **Permission settings**: Private/shared/public per item

### Atomic
- **Section-based UI**: Headings become navigation anchors
- **Split view**: Outline sidebar shows document structure
- **Drag-to-extract**: Create new objects from sections
- **Visual link graph**: Interactive graph with zoom/pan
- **Wikilink syntax**: [[links]] with autocomplete
- **Inline preview**: Hover over links to see content
- **Quick reference**: Copy markdown links easily

### Discoverable
- **Fuzzy search**: Typo-tolerant by default
- **Search-as-you-type**: Live results without hitting enter
- **Faceted filters**: Tags, dates, types, pipelines as checkboxes
- **Semantic search toggle**: Switch between keyword and semantic
- **Recent searches**: Dropdown with search history
- **Saved searches**: Pin common queries
- **Search suggestions**: Based on usage patterns

### Evergreen
- **Resurfacing dashboard**: "Items to revisit" on home screen
- **Related items sidebar**: Automatic suggestions
- **Backlinks section**: Show what links to current object
- **Version history**: Timeline view of all edits
- **Visual diff**: Side-by-side comparison of versions
- **Staleness indicators**: Age badges like "3 months old, no updates"
- **Smart notifications**: Gentle reminders to review old content

### Actionable
- **Composition menu**: "Create from this" → brief/task/draft
- **Task extraction**: One-click checkbox creation from content
- **Quick tagging**: Dropdown for decision/action tags
- **Export modal**: Preview before downloading (MD/HTML/JSON/PDF)
- **Archive workflow**: Confirmation with undo option
- **Action shortcuts**: Keyboard bindings for common workflows

---

## User Interface Structure

### Layout Components

**Global Navigation**
- Top bar with logo, global search, profile menu
- Left sidebar: Navigation (Home, Search, Graph, Jobs, Settings)
- Right sidebar: Contextual info (details, related items, backlinks)
- Main content area: Responsive to sidebar visibility
- Bottom status bar: Job indicators, connection status

**Views**
- **Home/Dashboard**: Resurfaced items, recent captures, activity feed
- **Search**: Full-screen search with filters and results
- **Object Detail**: Single object view with tabs (Content, Metadata, Graph, History)
- **Graph View**: Interactive visualization of entity relationships
- **Composition**: Template-based output generation interface
- **Jobs**: Background job monitoring and management
- **Settings**: User preferences and configuration

### Component Requirements

**Search Interface**
```
┌─────────────────────────────────────────────────────┐
│  🔍 [Search knowledge...]           [Filters ▼]     │
├─────────────────────────────────────────────────────┤
│  ☑ Fuzzy  ☐ Semantic  |  Tags: design, ux           │
│  Type: all ▼  |  After: [date]  |  Sort: relevance  │
├─────────────────────────────────────────────────────┤
│  📄 Authentication Flow Design                      │
│     mentions: @stripe.api, @ui.form                 │
│     3 days ago · pipeline: text.long                │
├─────────────────────────────────────────────────────┤
│  📄 Checkout Error Handling                         │
│     mentions: @stripe.api                           │
│     1 week ago · pipeline: url.article              │
└─────────────────────────────────────────────────────┘
```

**Object Detail View**
```
┌───────────────┬─────────────────────────────────────────┐
│               │  Authentication Flow Design             │
│  Navigation   │  ───────────────────────────────────    │
│               │  📝 Content  🏷️ Tags  🔗 Graph  📊 Meta │
│  • Overview   ├─────────────────────────────────────────┤
│  • Search     │                                         │
│  • Graph      │  [Rich content display with sections]  │
│  • Jobs       │                                         │
│               │  ## Overview                            │
│               │  This document covers...                │
│               │                                         │
│               │  ## Implementation                      │
│               │  Step 1: ...                            │
│               │                                         │
├───────────────┼─────────────────────────────────────────┤
│  Related (5)  │  [Bottom action bar]                    │
│  • Item 1     │  ⎘ Share  📤 Export  🗃️ Archive        │
│  • Item 2     └─────────────────────────────────────────┘
└───────────────┘
```

**Graph Visualization**
```
┌─────────────────────────────────────────────────────┐
│  [Authentication Flow] ──mentions──> [@stripe.api]  │
│         │                                            │
│         │                                            │
│    related-to                                        │
│         │                                            │
│         ▼                                            │
│  [Checkout Flow] ──mentions──> [@ui.form]           │
│                                       │              │
│                                  mentions            │
│                                       │              │
│                                       ▼              │
│                              [@ui.validation]        │
└─────────────────────────────────────────────────────┘
```

---

## Technical Architecture

### Technology Stack

**Required Capabilities:**
- Modern JavaScript framework (React/Vue/Svelte or similar)
- TypeScript for type safety
- Component-based architecture
- Client-side routing (SPA)
- State management solution
- WebSocket support for real-time updates
- Responsive CSS framework or utility library
- Markdown rendering with syntax highlighting
- Graph visualization library (D3, Cytoscape, or similar)
- Accessibility testing tools integration

**Not Prescriptive:**
This document does not mandate specific frameworks or libraries. Implementation teams should choose tools that:
- Support the accessibility requirements
- Provide excellent developer experience
- Have active maintenance and community
- Align with team expertise
- Meet performance requirements

### API Integration

**REST API** (primary)
- `POST /analyze` - Submit content for ingestion
- `GET /objects` - List knowledge objects with filters
- `GET /objects/{id}` - Get single object details
- `GET /search` - Execute search query
- `GET /entities` - List entities
- `GET /entities/{slug}/backlinks` - Get entity relationships
- `GET /jobs` - List background jobs
- `GET /jobs/{id}` - Get job status
- `POST /compose` - Generate composition output

**WebSocket** (optional, for real-time)
- Job progress updates
- Live search results
- Collaborative editing events
- Notification delivery

**GraphQL** (future consideration)
- Single endpoint for flexible queries
- Reduce over-fetching
- Batch related queries
- Schema introspection

---

## State Management

### Local State
- UI state (sidebar visibility, active tab, scroll position)
- Form state (unsaved changes, validation errors)
- Transient notifications

### Server State
- Knowledge objects (cached, invalidated on mutation)
- Search results (cached per query)
- Entity graph data
- Job status

### User Preferences
- Theme (dark/light/auto)
- Language preference
- Layout density (compact/normal/comfortable)
- Default filters
- Saved searches
- Keyboard shortcut customizations

**Storage:**
- LocalStorage for user preferences
- SessionStorage for temporary state
- IndexedDB for offline capability (future)

---

## Performance Requirements

### Load Time
- **Initial load**: < 3 seconds on 3G
- **Time to interactive**: < 5 seconds on 3G
- **Subsequent navigations**: < 500ms (client-side routing)

### Interaction
- **Search response**: < 200ms (perceived, with debouncing)
- **UI updates**: 60fps for animations
- **Large lists**: Virtual scrolling for 1000+ items
- **Graph rendering**: Progressive loading for 100+ nodes

### Bundle Size
- **Initial bundle**: < 200KB gzipped
- **Lazy loading**: Route-based code splitting
- **Image optimization**: WebP with fallbacks
- **Font loading**: Subset fonts, display swap

### Caching Strategy
- **Static assets**: Long-term cache with content hashing
- **API responses**: Stale-while-revalidate pattern
- **Search results**: Cache with TTL
- **Images**: Service worker caching

---

## Security Considerations

### Authentication
- Support multiple auth methods (configurable)
- Token-based authentication (JWT or similar)
- Refresh token rotation
- Logout clears all local state

### Authorization
- Respect privacy flags from server
- No client-side bypass of permissions
- Audit log for sensitive actions

### Data Protection
- HTTPS only (no mixed content)
- Content Security Policy headers
- XSS prevention (escape user content)
- CSRF protection for mutations
- Input sanitization for rich text

### Privacy
- No third-party analytics without consent
- Local-first by default (future: offline mode)
- Clear data retention policies
- Export functionality for data portability

---

## Accessibility Checklist

### Keyboard Navigation
- [ ] All interactive elements focusable
- [ ] Logical tab order
- [ ] Keyboard shortcuts documented in help
- [ ] No keyboard traps
- [ ] Visible focus indicators
- [ ] Esc key closes modals/dialogs

### Screen Reader Support
- [ ] Semantic HTML (nav, main, article, aside)
- [ ] ARIA landmarks for page regions
- [ ] ARIA labels for icons and images
- [ ] ARIA live regions for dynamic updates
- [ ] Form labels and error associations
- [ ] Skip links to main content
- [ ] Heading hierarchy (h1 → h6 properly nested)

### Visual Design
- [ ] Color contrast ≥ 4.5:1 for text
- [ ] Color contrast ≥ 3:1 for UI components
- [ ] No information conveyed by color alone
- [ ] Text resizable to 200% without loss
- [ ] Touch targets ≥ 44×44 CSS pixels
- [ ] Hover/focus states clearly visible

### Content
- [ ] Alt text for meaningful images
- [ ] Captions for video content
- [ ] Transcripts for audio content
- [ ] Clear error messages with recovery suggestions
- [ ] Consistent navigation across views
- [ ] Help documentation accessible

### Testing
- [ ] Automated testing (axe, Lighthouse)
- [ ] Manual keyboard testing
- [ ] Screen reader testing (NVDA, JAWS, VoiceOver)
- [ ] Browser zoom testing (up to 200%)
- [ ] Color blindness simulation
- [ ] High contrast mode testing

---

## Mobile Considerations

### Responsive Breakpoints
- **Mobile**: < 768px (single column, stacked layout)
- **Tablet**: 768px - 1024px (collapsible sidebars)
- **Desktop**: > 1024px (full multi-pane layout)

### Touch Interactions
- Swipe gestures for navigation (back, forward)
- Pull-to-refresh on search results
- Long-press for context menus
- Pinch-to-zoom on graph view
- Touch targets ≥ 44×44 CSS pixels

### Mobile-Specific Features
- Share sheet integration (if PWA)
- Camera capture for images
- Voice input for search and capture
- Offline mode with sync queue (future)
- Home screen app icon (PWA manifest)

---

## Progressive Web App (PWA) Requirements

### Manifest
- App name, short name, description
- Icons (192×192, 512×512)
- Theme color and background color
- Display mode: standalone
- Start URL

### Service Worker (future phase)
- Offline fallback page
- Cache API responses
- Background sync for captures
- Push notifications for resurfacing

### Installation
- Install prompt for eligible users
- Standalone display mode support
- Splash screen on mobile

---

## Internationalization (i18n)

### UI Translations
- User-facing strings extracted to translation files
- Support for multiple languages (en, fr, ar, es, etc.)
- RTL layout support (mirror UI for Arabic/Hebrew)
- Date/time formatting per locale
- Number formatting per locale

### Content Languages
- Display content in original language
- Show translations when available (from backend)
- Language switcher for UI
- Respect browser language preferences

### Pluralization
- Support for plural forms per language
- ICU MessageFormat or similar

---

## Error Handling

### User-Facing Errors
- Clear, actionable error messages
- Avoid technical jargon
- Suggest recovery actions
- Provide help links when appropriate

### Error Boundaries (React example)
- Catch rendering errors
- Display fallback UI
- Log errors for debugging
- Allow user to retry or navigate away

### Network Errors
- Detect offline state
- Queue actions for retry when online
- Show connection status indicator
- Graceful degradation

### Validation Errors
- Inline field-level validation
- Summary of errors at top of form
- Accessible error announcements
- Focus on first error field

---

## Testing Strategy

### Unit Tests
- Component logic
- State management
- Utility functions
- Data transformations

### Integration Tests
- API client interactions
- State updates from API responses
- User workflows across components

### End-to-End Tests
- Critical user paths (capture, search, view, share)
- Cross-browser compatibility
- Responsive design validation
- Accessibility compliance

### Performance Tests
- Load time benchmarks
- Interaction responsiveness
- Memory usage profiling
- Bundle size monitoring

### Accessibility Tests
- Automated scanning (axe)
- Manual keyboard navigation
- Screen reader compatibility
- WCAG compliance audit

---

## Design System Requirements

### Principles
- Consistency across all views
- Clear visual hierarchy
- Generous whitespace
- Accessible color palette
- Scalable typography system

### Components Needed
- Buttons (primary, secondary, ghost, danger)
- Input fields (text, textarea, select, checkbox, radio)
- Cards (for object listings)
- Modals/Dialogs
- Toasts/Notifications
- Loading indicators (spinner, skeleton, progress bar)
- Badges (tags, privacy indicators, counts)
- Tables (for metadata display)
- Tabs (for object detail views)
- Dropdowns/Select menus
- Search input with autocomplete
- Rich text editor
- Graph visualization container
- Sidebars (collapsible)
- Navigation bars
- Breadcrumbs
- Empty states
- Error states

### Design Tokens
- Colors (primary, secondary, accent, neutral, semantic)
- Typography (font families, sizes, weights, line heights)
- Spacing scale (4px base grid)
- Border radius values
- Shadow values
- Z-index scale
- Transitions and animations

---

## Future Enhancements

### Planned Features (not required for MVP)
- Offline mode with sync queue
- Real-time collaborative editing
- Advanced graph visualizations (force-directed, hierarchical)
- Plugin system for custom views
- Customizable dashboard widgets
- Export templates with custom formatting
- Bulk operations (tag, archive, share multiple items)
- Advanced search query builder UI
- Integration with external tools (via browser extension)
- Mobile-native apps (iOS/Android)

---

## Documentation Requirements

### For Users
- Getting started guide
- Feature tour (interactive walkthrough)
- Keyboard shortcuts reference
- Search query syntax help
- Composition templates documentation
- Privacy and sharing explained
- FAQ and troubleshooting

### For Developers
- Setup and development guide
- Component library documentation
- API integration guide
- State management patterns
- Testing approach
- Deployment process
- Contributing guidelines

---

## Success Metrics

### User Experience
- Time to first capture: < 30 seconds from landing
- Search success rate: > 80% find what they need in first 3 results
- Return user rate: > 60% return within 7 days
- Feature adoption: 50% use composition features within 30 days

### Performance
- Lighthouse score: > 90 for all categories
- Time to Interactive: < 5s on 3G
- First Contentful Paint: < 2s on 3G
- Cumulative Layout Shift: < 0.1

### Accessibility
- Zero critical accessibility issues (axe)
- WCAG 2.1 AA compliance: 100%
- Keyboard navigation: All features accessible
- Screen reader compatibility: Tested with 3+ screen readers

---

## See Also

**Core Principles:**
- [non-negotiables.md](./non-negotiables.md) - Core design principles
- [interface-mappings.md](./interface-mappings.md) - Cross-interface implementation guide

**Related Interfaces:**
- [tui.md](./tui.md) - Terminal UI implementation
- [api-cli.md](./api-cli.md) - CLI interface

**Architecture:**
- [../architecture.md](../architecture.md) - System architecture
- [../design.md](../design.md) - Design patterns
- [pipelines.md](./pipelines.md) - Pipeline system
- [testing.md](./testing.md) - Testing strategy

---

**Status:** This document defines requirements for the WebUI. Design and implementation details will be specified during the design phase based on these requirements.

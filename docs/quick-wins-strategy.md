# Quick Win Strategy: "See Value in 5 Minutes"

Based on the personas documentation, here's what we should provide for **immediate value before users invest time in deep integration**.

## Architecture Note
ctxt has no plugin system ([ADR-078](decisions/ADR-078-no-plugin-system.md)). Import sources and file handlers below are ctxt built-ins, shipped or planned; processing is extended with custom pipelines and steps installed from registries.

## Zero-Config Demo Mode
- **For:** Elena, Julian, Priya (low tech savvy)
- **What:** Pre-populated demo database with sample queries
- **Why:** Show what's possible before asking them to import anything
- **Command:** `ctxt demo [scenario]` - Core command with built-in scenarios
- **Example:** `ctxt demo research` loads sample research PDFs, lets them ask "Find connections between documents about X" instantly

## Local Folder/Archive Import (One-Shot)
- **For:** Elena, Julian, Alex, Marcus (file-heavy users, researchers, developers)
- **What:** Import entire folder or ZIP/TAR archive in one command
- **Why:** Immediate bulk ingestion of existing knowledge collections
- **Command:** `ctxt import folder ~/Documents/Research` or `ctxt import archive ~/backup.zip`
- **Value:** "Make 1000+ files searchable" in <3 minutes

## Browser Bookmark Import (1-Click)
- **For:** Sarah, Victor, Morgan (browser-heavy workflows)
- **What:** Import bookmarks from any browser
- **Why:** They already have 1000+ bookmarks; instant searchable knowledge base
- **Command:** `ctxt import bookmarks --browser chrome` (built-in importer)
- **Value:** "Search my bookmarks using natural language" in <60 seconds

## Folder Watching (Continuous Sync)
- **For:** Elena, Julian, Alex (file-heavy users who keep adding content)
- **What:** Continuously watch folder for new/changed files
- **Why:** Passive, automatic value from ongoing file collections
- **Command:** `ctxt watch ~/Documents/Research` - Core command with built-in handlers
- **Value:** "Auto-index my PDFs/images/code as I add them" - set once, passive forever

## GitHub Stars Import
- **For:** Marcus, Alex, Devin (developers)
- **What:** Import all starred GitHub repositories
- **Why:** 500+ starred repos become queryable ("Show me all Rust web frameworks I've starred")
- **Command:** `ctxt import github-stars --username jadb` (built-in importer)
- **Value:** Instant code reference library

## Pocket/Instapaper/ReadItLater Import
- **For:** Sarah, Victor, Morgan (read-it-later users)
- **What:** Import from read-it-later services
- **Why:** Years of saved articles become searchable
- **Command:** `ctxt import pocket --api-key <key>` (built-in importer)
- **Value:** "Finally answer 'where did I read that article about X?'"

## Notion Workspace Import (Read-Only)
- **For:** Morgan, Priya, Ian (Notion power users)
- **What:** OAuth integration to sync Notion workspace
- **Why:** Their existing knowledge base becomes AI-queryable
- **Command:** `ctxt import notion --workspace <id>` (built-in importer)
- **Value:** "Ask questions about our company wiki" without migration

## Public Taxonomies/Registries
- **For:** Tom, Elias, Rex (taxonomy users)
- **What:** Pre-loaded taxonomies (medical terms, security concepts, legal vocabulary)
- **Why:** Show the power of structured knowledge without setup
- **Command:** `ctxt registry add <url>` (already implemented!)
- **Example:** Medical researcher gets PubMed taxonomy instantly; no configuration needed

## Newsletter Email Auto-Forward Address
- **For:** Victor, Morgan, Tom (information consumers)
- **What:** Give them `yourusername@ctxt.help` email
- **Why:** Forward newsletters = instant knowledge capture without changing behavior
- **Implementation:** Backend service → triggers `ctxt analyze` on email receipt
- **Value:** "All my newsletters become searchable" - passive value

## Audio Recording → Transcript (Mobile App)
- **For:** Paul, Morgan (meeting/interview heavy)
- **What:** Mobile app with "Record & Transcribe" button
- **Why:** Immediate utility without desktop setup
- **Implementation:** Mobile app → uploads to backend → triggers ingestion pipeline
- **Value:** "Record this meeting, search it later" - instant ROI

## "Try Without Installing" Web Playground
- **For:** Everyone (commitment-phobic users)
- **What:** Hosted sandbox with sample data
- **Why:** Test queries, see UI, understand value proposition
- **Implementation:** Web app with embedded demo scenarios
- **Value:** "Experience it in 30 seconds" before downloading CLI

---

## Priority Matrix

| Feature | Setup Time | User Impact | Technical Complexity | Where |
|---------|-----------|-------------|---------------------|-------------|
| **Demo mode** | 0 sec | MEDIUM | Low | Core |
| **Local folder import** | <1 min | HIGH | Low | Core |
| **Archive import (zip/tar)** | <1 min | HIGH | Low | Core |
| **Browser bookmark import** | <1 min | HIGH | Low | Core importer |
| **Folder watch** | <2 min | HIGH | Medium | Core |
| **GitHub stars import** | <1 min | HIGH | Low | Core importer |
| **Public taxonomies** | 0 sec | MEDIUM | Low | Core (exists!) |
| **Pocket import** | <1 min | MEDIUM | Low | Core importer |
| **Notion sync** | <2 min | HIGH | High | Core importer |
| **Newsletter email forward** | <30 sec | HIGH | Medium | Backend Service |
| **Web playground demo** | 0 sec | MEDIUM | High | Web App |
| **Audio recording app** | 0 sec (app install) | HIGH | Medium | Mobile App |

---

## Anti-Pattern to Avoid

**Don't ask users to:**
1. Configure YAML files before seeing value
2. Understand taxonomies, weights, or agents upfront
3. Manually tag their first 100 bookmarks
4. Set up local LLM infrastructure initially (offer cloud trial)
5. Choose storage backends (SQLite vs JSON vs Postgres) before they understand the tool

---

## The "First 5 Minutes" User Journey

```
1. Install CLI → Run `ctxt demo research` OR `ctxt import folder ~/Documents`
2. Try natural language query: `ctxt find "topic I care about"`
3. See instant, relevant results from THEIR data (or realistic demo)
4. NOW they're willing to:
   - Connect more sources (GitHub, Notion, Pocket, bookmarks)
   - Set up folder watching for continuous sync
   - Forward newsletters to email address
   - Explore advanced features (profiles, registries, compositions)
```

**Key Principle:** Show, don't tell. Value before configuration. Their data before theoretical capabilities.

## Extension Points

Built-in commands are fixed; nothing registers new subcommands, flags or hooks at runtime. Processing is extended through:

- **Custom pipelines** (`dpkms pipeline create`) and **pipeline overrides** (`pipelines.overrides`)
- **Steps installed from a step registry** (`dpkms pipeline step install`)
- **Registries** for entities and taxonomies
- **External hooks** for other applications ([ADR-075](decisions/ADR-075-external-hook-surface.md), designed)

---

## Implementation Recommendations

### Phase 1: Core Framework + Essential Imports (Week 1-2)
**Core CLI:**
- `ctxt import <source>` command framework
- `ctxt import folder <path>` - local folder import (core)
- `ctxt import archive <file>` - ZIP/TAR import (core)
- `ctxt demo [scenario]` framework with scenario loader

**Essential importers:**
- Bookmarks: Chrome, Firefox, Safari support
- GitHub: Stars import, repo metadata
- Demo scenarios: research, developer, bookmarks

### Phase 2: Continuous Sync + Passive Capture (Week 3-4)
**Core CLI:**
- `ctxt watch <path>` command framework with handler registration
- File system monitoring and change detection

**Steps & Services:**
- File handler steps: PDF extractor, image OCR, code analyzer
- Newsletter email forwarding service → `ctxt analyze` integration
- Browser extension for one-click capture

### Phase 3: Integration Layer (Week 5-8)
**Importers:**
- Pocket/Instapaper importer with API integration
- Notion importer with OAuth and read-only sync
- Raindrop.io importer for bookmark sync

**Infrastructure:**
- Web playground/sandbox environment with demo scenarios
- Public taxonomy registry (leverage existing `ctxt registry` command)

### Phase 4: Advanced Workflows (Week 9+)
**Mobile & Advanced:**
- Audio recording & transcription mobile app
- Mobile app for on-the-go capture
- Advanced file handlers (video, audio, specialized formats)
- Deep customization UI (profiles, weights, custom taxonomies)

---

## Success Metrics

**Time to First Value:**
- Target: <5 minutes from install to first useful query result
- Measure: Time from `ctxt --version` to first successful `ctxt find <query>` with relevant results

**Activation Rate:**
- Target: >70% of users who install actually complete one import or demo
- Measure: Users who run `ctxt demo` or `ctxt import` within 24 hours

**Retention Indicator:**
- Target: >50% return within 7 days to query their data
- Measure: Users who run `ctxt find` or `ctxt list` at least twice in first week

**Importer Adoption:**
- Target: >40% run at least one source importer within first week
- Measure: Users who run `ctxt import <source>` successfully

---

## Related Documentation

- [End-User Personas](./ctxt/personas/personas-end-users.md)
- [User Roles Personas](./ctxt/personas/personas-user-roles.md)
- [User Stories](./ctxt/user-stories.md)
- [Non-Negotiables](./ctxt/non-negotiables.md)

# Persona: Knowledge Workers / Context-Seeking Professionals

**Primary Role:** Humans seeking just-in-time context (researchers, product managers, engineers, designers, decision-makers)

---

## Goals

- Capture insights and content with minimal friction (seconds, not minutes)
- Retrieve just-in-time context matching role/project focus without scrolling through archives
- Discover relationships and patterns across accumulated knowledge automatically
- Generate briefs, plans, and drafts with traceable sources (who said this, when, why)
- Iterate and refine searches naturally without learning query syntax

---

## Interaction Pattern

### Capture Path

#### Minimal Friction Input
```bash
ctxt add "Here's my insight about error handling..."  # Typed
ctxt add < document.pdf                              # Piped
ctxt add --file ~/Notes/meeting-notes.md             # File
# or: paste via TUI, browser extension, or mobile
```

#### System Processing (Async in Background)
1. `ctxt` normalizes input and detects type (text, URL, image, audio, video, document, feed)
2. Selects pipeline based on type:
   - `text.short` → quick summarization, entity extraction, tag assignment
   - `text.long` → deep analysis, section decomposition, decision extraction
   - `url.article` → fetch, clean HTML, extract article content
   - `image.diagram` → OCR, diagram understanding, entity extraction
   - `video.transcription` → frame sampling + audio transcription
3. Applies focus profile context (role/project filters)
4. Creates a Job in pending state
5. Returns immediately (no blocking); user can continue work
6. Worker picks up job, executes pipeline steps, commits results
7. Knowledge object stored with: summary, sections, tags, mentions, decisions, embeddings

#### Optional: Apply Focus Profile
```bash
ctxt add "..." --profile engineering    # Apply "engineering" role lens
ctxt add "..." --project "mobile-app"   # Apply "mobile-app" project lens
```

### Search Path

#### Natural Language by Default
```bash
ctxt find "what are insights on error handling"
ctxt search "how does the system handle user errors"
ctxt find "recent decisions about mobile"
ctxt search "related work on async patterns"
```

**System Auto-Detection:**
- Intent: Semantic summary, Relationship pattern, Temporal entity, Similarity discovery, Metadata filter
- Primary Strategy: Vector (similarity), Graph (traversal), Metadata (date filter), FTS (keyword)
- Secondary Strategy: Backup if primary is slow or empty

**Result Presentation:**
- Top 10 results by relevance
- Each result includes: object summary, why it matched (contributing factors), source pipeline
- Optional: `rank.explain` payload (weighted components, strategy contributions)

#### Structured Query (Power User Mode)
```bash
ctxt find "type==decision;tags=in=urgent;created_at>2025-01-01"  # RSQL
```
- Deterministic results, useful for reproducible reports
- Less natural but more explicit

#### Refinement & Clarification
- Chat-like iterations: `"show only engineering decisions"`, `"what's the latest?"`, `"from which teams?"`
- System learns focus context from conversation history
- Can toggle between natural and structured queries as needed

### Composition Path

#### Generate Briefs
```bash
ctxt make brief --from decision-123,decision-456
ctxt make brief --tag urgent --pipeline text.long
ctxt make brief --project mobile-app --days 7
```

**What Happens:**
1. System queries knowledge base using filters (IDs, tags, pipeline, date)
2. Retrieves objects + graph neighbors (entities, mentions, related decisions)
3. Selects template based on profile + object types
4. Assembles atomic nodes following entity relationships (who, what, when, why, impact)
5. Applies template rules (sections, formatting, emphasis)
6. Generates human-readable brief with provenance

**Output Example:**
```
## Brief: Recent Mobile Architecture Decisions

### Context
- Related decisions: D-1, D-2, D-3 (from Product & Engineering)
- Mentioned stakeholders: Alice (PM), Bob (Architect), Carol (QA Lead)
- Time period: Last 2 weeks

### Key Decisions
1. Switched to Swift-first mobile strategy (Decision D-1, 2025-01-15)
   - Impact: HIGH | Status: OPEN
   - Rationale: Faster iteration, native performance
   - Related: D-2 (deprecate Objective-C), D-3 (plan SDK rewrite)

2. Defer tablet support to Q2 (Decision D-2, 2025-01-18)
   - Impact: MEDIUM | Status: OPEN
   - Consequence: Frees engineering bandwidth for iOS refactor

### Mentions
- Cross-platform compatibility (raised 3x)
- Testing strategy for new Swift stack (raised 2x)
- Customer support impact (raised 1x)

### Sources
- Decision D-1: engineering meeting notes (Jan 15)
- Decision D-2: email from Alice (Jan 18)
- Mentions: Slack #mobile channel (Jan 15-18)

Generated: Jan 18, 2025 | Profile: Mobile PM
```

#### Generate Plans
```bash
ctxt make plan --from decision-123
ctxt make plan --intent "implement mobile-first redesign"
```

**What Happens:**
1. Parse intent or source decisions
2. Extract related tasks and decisions
3. Order by dependency (prerequisites first)
4. Assign owners based on mention patterns
5. Generate actionable plan with acceptance criteria

### Composition Iteration
- Request edits: `"combine the architecture section"`, `"add mobile metrics"`
- Request export: `"save as PDF"`, `"export to Markdown"`
- Request review: `"who should review this plan?"` (system suggests stakeholders from mentions)

---

## Key Pain Points

- **Query Ambiguity:** Natural language intent classification may be wrong (query "recent decisions about X" might return articles instead)
- **Profile Setup Friction:** Defining role/project lenses feels like admin work; unclear when to use them
- **Slow Background Jobs:** Waiting for enrichment to complete before results are useful (especially for long documents)
- **Context Fragmentation:** Knowledge scattered across email, Slack, documents, PDFs; capturing everything is tedious
- **Over-Generalization:** Default pipelines may miss domain-specific nuances (e.g., code repositories, design files)
- **No Collaboration:** Knowledge is siloed per user; team context sharing requires manual exports
- **Search Surprise:** Multi-strategy results merge in unexpected ways; reranking may prioritize wrong sources

---

## System Leverage

### Focus Profiles (Role/Project Lenses)
- Define: `"engineering"` profile → emphasizes decisions, technical discussions, code patterns
- Use at capture: enrichment focuses on extraction relevant to engineering
- Use at query: results boosted if relevant to engineering, filtered if off-topic
- Use at composition: brief templates emphasize stakeholders, decisions, technical rationale

### Just-In-Time Surfacing
- System learns user search history and focus context
- Can suggest contextual insights: `"Related: Decision D-5 from last month"`, `"FYI: New knowledge on async patterns"`
- Optional background notifications (desktop, Slack, email)

### Multimodal Ingestion
- Text: typed, pasted, imported from files
- URLs: articles, repositories, APIs (fetched, processed, stored)
- Images: diagrams, screenshots, handwritten notes (OCR'd, analyzed)
- Audio: meeting transcripts, podcast segments (transcribed, indexed)
- Videos: presentation recordings, demos (transcribed, scene-detected)
- Documents: PDFs, Markdown, code files (parsed, structured)
- Feeds: RSS/Atom (periodic sync, incremental indexing)

### Semantic Enrichment
- Automatic summarization (extractive or abstractive)
- Entity extraction (`@person.name`, `@project.name`, `@system.name`)
- Decision extraction (what was decided, impact, status)
- Task extraction (actionable items with owners)
- Mention extraction (cross-references between objects)
- No manual tagging (system derives from content)

### Federated Registries
- Access external knowledge without data movement
- Registries provide: taxonomy, tag vocabulary, pre-computed embeddings, domain expertise
- Search can include or exclude registry results per query
- Conflict resolution: user can choose canonical source when duplicates appear

---

## User Stories

Knowledge workers interact with the system through these key stories:

### Platform-Specific Capture
- [US-0200](../stories/capture/US-0200-browser-cookie-bridge.md) — Browser Cookie Bridge (authenticated capture)
- [US-0201](../stories/capture/US-0201-x-twitter-capture.md) — X/Twitter Capture
- [US-0203](../stories/capture/US-0203-arxiv-capture.md) — ArXiv Capture
- [US-0204](../stories/capture/US-0204-linkedin-capture.md) — LinkedIn Capture
- [US-0205](../stories/capture/US-0205-wikipedia-capture.md) — Wikipedia Capture
- [US-0207](../stories/capture/US-0207-web-tab-capture.md) — Web Tab Capture
- [US-0209](../stories/capture/US-0209-authenticated-web-fetch.md) — Authenticated Web Fetch

### Capture & Ingestion
- [US-0001](../stories/ingestion/US-0001-text-capture-minimal-friction.md) — Text Capture with Minimal Friction
- [US-0002](../stories/ingestion/US-0002-url-capture-and-extraction.md) — URL Capture and Extraction
- [US-0003](../stories/ingestion/US-0003-image-ocr-and-analysis.md) — Image OCR and Analysis
- [US-0004](../stories/ingestion/US-0004-audio-transcription-and-indexing.md) — Audio Transcription and Indexing
- [US-0005](../stories/ingestion/US-0005-video-processing-with-scenes.md) — Video Processing with Scenes
- [US-0006](../stories/ingestion/US-0006-document-parsing-and-decomposition.md) — Document Parsing and Decomposition
- [US-0007](../stories/ingestion/US-0007-feed-ingestion-and-sync.md) — Feed Ingestion and Sync

### Enrichment & Understanding
- [US-0009](../stories/enrichment/US-0009-extract-entities-and-mentions.md) — Extract Entities and Mentions (automatic understanding)
- [US-0010](../stories/enrichment/US-0010-extract-decisions-and-tasks.md) — Extract Decisions and Tasks (actionable insights)
- [US-0011](../stories/enrichment/US-0011-assign-tags-from-vocabulary.md) — Assign Tags from Vocabulary (automatic categorization)
- [US-0012](../stories/enrichment/US-0012-generate-summaries-and-sections.md) — Generate Summaries and Sections (quick understanding)
- [US-0047](../stories/enrichment/US-0047-extract-temporal-information.md) — Extract Temporal Information (timelines)
- [US-0048](../stories/enrichment/US-0048-detect-sentiment-and-tone.md) — Detect Sentiment and Tone (context awareness)

### Search & Discovery
- [US-0016](../stories/search/US-0016-natural-language-search.md) — Natural Language Search (just ask in plain English)
- [US-0019](../stories/search/US-0019-federated-registry-search.md) — Federated Registry Search (external knowledge)
- [US-0020](../stories/search/US-0020-apply-focus-profile-to-search.md) — Apply Focus Profile to Search (role-based results)
- [US-0021](../stories/search/US-0021-search-with-result-explanation.md) — Search with Result Explanation (why did this match?)
- [US-0051](../stories/search/US-0051-semantic-search-with-embeddings.md) — Semantic Search with Embeddings (conceptual search)
- [US-0053](../stories/search/US-0053-cross-profile-search-aggregation.md) — Cross-Profile Search Aggregation (multiple roles)
- [US-0054](../stories/search/US-0054-saved-search-and-alerts.md) — Saved Search and Alerts (stay updated)
- [US-0055](../stories/search/US-0055-search-history-and-recommendations.md) — Search History and Recommendations (smart suggestions)

### Composition & Communication
- [US-0022](../stories/composition/US-0022-generate-brief-from-objects.md) — Generate Brief from Objects (compile insights)
- [US-0023](../stories/composition/US-0023-generate-plan-from-decisions.md) — Generate Plan from Decisions (action planning)
- [US-0025](../stories/composition/US-0025-export-brief-to-markdown-pdf.md) — Export Brief to Markdown/PDF (share findings)
- [US-0026](../stories/composition/US-0026-share-composition-with-team.md) — Share Composition with Team (collaboration)
- [US-0056](../stories/composition/US-0056-compose-decision-timeline.md) — Compose Decision Timeline (track history)
- [US-0057](../stories/composition/US-0057-compose-stakeholder-analysis.md) — Compose Stakeholder Analysis (who's involved?)
- [US-0058](../stories/composition/US-0058-compose-impact-assessment.md) — Compose Impact Assessment (consequences)
- [US-0059](../stories/composition/US-0059-compose-recommendation-document.md) — Compose Recommendation Document (guide action)
- [US-0060](../stories/composition/US-0060-compose-with-custom-template.md) — Compose with Custom Template (personalized formats)

### Configuration & Preferences
- [US-0030](../stories/admin/US-0030-set-up-focus-profiles.md) — Set Up Focus Profiles (role/project lenses)

### Import & Migration
- [US-0301](../stories/ingestion/US-0301-import-chrome-bookmarks.md) — Import Chrome Bookmarks
- [US-0302](../stories/ingestion/US-0302-import-edge-bookmarks.md) — Import Edge Bookmarks
- [US-0303](../stories/ingestion/US-0303-import-firefox-bookmarks.md) — Import Firefox Bookmarks
- [US-0304](../stories/ingestion/US-0304-import-safari-bookmarks.md) — Import Safari Bookmarks
- [US-0305](../stories/ingestion/US-0305-import-google-drive.md) — Import Google Drive
- [US-0306](../stories/ingestion/US-0306-import-onedrive.md) — Import OneDrive
- [US-0307](../stories/ingestion/US-0307-import-notion.md) — Import Notion
- [US-0308](../stories/ingestion/US-0308-import-dropbox.md) — Import Dropbox
- [US-0309](../stories/ingestion/US-0309-import-slack.md) — Import Slack
- [US-0310](../stories/ingestion/US-0310-import-discord.md) — Import Discord
- [US-0311](../stories/ingestion/US-0311-import-obsidian-vault.md) — Import Obsidian Vault
- [US-0312](../stories/ingestion/US-0312-import-logseq-graph.md) — Import Logseq Graph
- [US-0313](../stories/ingestion/US-0313-import-evernote-enex.md) — Import Evernote ENEX
- [US-0314](../stories/ingestion/US-0314-import-pinboard-bookmarks.md) — Import Pinboard Bookmarks
- [US-0315](../stories/ingestion/US-0315-import-raindrop-bookmarks.md) — Import Raindrop Bookmarks
- [US-0316](../stories/ingestion/US-0316-import-twitter-archive.md) — Import Twitter Archive
- [US-0317](../stories/ingestion/US-0317-import-linkedin-export.md) — Import LinkedIn Export

---

## Success Metrics

- **Capture friction:** Time from decision to indexed (goal: <10 seconds)
- **Search relevance:** % of top-5 results judged relevant by user
- **Profile adoption:** % of users with >1 focus profile, % of queries using profile context
- **Composition confidence:** % of generated briefs used without edits
- **Knowledge rediscovery:** % of searches that surface previously-seen knowledge
- **Collaboration velocity:** Time to sync knowledge across team

---

## Collaboration with Other Personas

- **Maintainers:** Knowledge workers request features ("easier tagging", "better search ranking") and report bugs
- **Agents/LLMs:** Agents consume knowledge written by knowledge workers; workers benefit from agent-curated insights
- **Integrators:** Knowledge workers benefit from custom pipelines (e.g., "slack-to-context" plugin, "figma-to-context" plugin)
- **Operations:** Workers report slow searches and enrichment delays; operations optimize infrastructure

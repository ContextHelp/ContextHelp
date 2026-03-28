# Persona: Researchers & OSINT Analysts

**Primary Role:** Intelligence gatherers, competitive analysts, academic researchers, security researchers, journalists, due diligence professionals

---

## Goals

- Build comprehensive profiles of people, companies, and topics from scattered public sources
- Capture authenticated web content (behind logins) without manual copy-paste
- Recover full article bodies from difficult layouts using domain-aware extraction rules and fallbacks
- Cross-reference entities across platforms (same person on X, LinkedIn, GitHub, Wikipedia)
- Track changes over time (profile updates, new publications, deleted content)
- Maintain provenance chain for every captured artifact (when, where, how obtained)
- Work with both public OSINT and legitimately-authenticated private content

---

## Interaction Pattern

### Capture Path

#### Browser-Mediated Capture (Primary)
```bash
# Browser extension shares session cookies with ctxt
# User right-clicks "Capture to ctxt" on any authenticated page
# Extension sends: page URL, rendered HTML, cookies, timestamp, auth method

# What the extension captures:
# - Full rendered DOM (not just source — includes JS-rendered content)
# - Structured metadata: title, author, timestamps, platform-specific fields
# - Screenshot of visible viewport (archival proof)
# - Authentication context: which session/profile was used (never stores credentials)
```

#### Platform-Specific Ingestion
```bash
# Social media profiles
ctxt capture x.com/@username                    # Capture profile + recent posts
ctxt capture linkedin.com/in/jane-doe           # Capture profile (requires auth cookies)
ctxt capture github.com/org/repo                # Capture repo metadata + README

# Academic sources
ctxt capture arxiv.org/abs/2401.12345           # Capture paper metadata + abstract
ctxt capture scholar.google.com/citations?user=xyz  # Capture publication list

# Company intelligence
ctxt capture crunchbase.com/organization/acme   # Capture company profile
ctxt capture sec.gov/cgi-bin/browse-edgar?CIK=0001234  # SEC filings

# Bulk capture from URL list
ctxt capture --from-file urls.txt               # One URL per line
ctxt capture --from-file urls.txt --parallel 4  # Parallel capture with rate limiting
```

#### Profile Building
```bash
# Aggregate captured artifacts into a unified entity profile
ctxt profile build @person.jane-doe             # Build from all captured sources
ctxt profile build @company.acme-corp           # Company profile aggregation
ctxt profile build @topic.supply-chain-attacks  # Topic aggregation

# Profile includes:
# - All captured artifacts linked to this entity
# - Cross-platform presence map (where this entity appears)
# - Timeline of captured snapshots (chronological)
# - Relationship graph (connected entities)
# - Confidence scores for entity resolution matches
```

#### Temporal Tracking (Change Detection)
```bash
# Watch for changes on a URL or profile
ctxt watch x.com/@username --interval 24h       # Check daily
ctxt watch linkedin.com/in/jane-doe --interval 7d  # Check weekly
ctxt watch github.com/org/repo --interval 1h    # Check hourly (active investigation)

# Configure change detection sensitivity
ctxt watch x.com/@username --detect content     # Any content change
ctxt watch x.com/@username --detect structural  # Layout/field changes only
ctxt watch x.com/@username --detect deletions   # Alert on removed content

# List active watches
ctxt watch list
ctxt watch list --entity @person.jane-doe

# View change history
ctxt watch diff x.com/@username --from 2026-01-01 --to 2026-02-01
```

#### Cross-Reference and Entity Resolution
```bash
# Discover cross-platform presence for an entity
ctxt osint entity @person.jane-doe              # Show all captured artifacts
ctxt osint entity @person.jane-doe --graph      # Show relationship graph
ctxt osint entity @person.jane-doe --timeline   # Chronological activity

# Manual entity linking (when auto-resolution is uncertain)
ctxt osint link @person.jane-doe-twitter @person.jane-doe-linkedin
ctxt osint unlink @person.jane-doe-twitter @person.jane-doe-linkedin

# Entity resolution confidence
ctxt osint resolve @person.jane-doe             # Show resolution candidates + scores
ctxt osint resolve --threshold 0.7              # Auto-merge above confidence threshold
```

### Search Path

#### OSINT-Specific Queries
```bash
# Search across all captured OSINT artifacts
ctxt find "jane doe security researcher"
ctxt find "companies mentioned alongside acme corp"
ctxt find "deleted tweets from @username"

# Structured OSINT queries
ctxt find "source_platform==twitter;entity==@person.jane-doe;captured_at>2026-01-01"
ctxt find "type==profile;source_platform=in=(linkedin,github);entity==@person.jane-doe"
ctxt find "has_been_deleted==true;source_platform==twitter"

# Temporal queries
ctxt find "entity==@person.jane-doe" --sort captured_at --order desc
ctxt find "entity==@company.acme-corp" --changed-since 2026-01-15
```

#### Provenance Queries
```bash
# Trace where an artifact came from
ctxt provenance show artifact-abc123
# → Source: https://x.com/@username/status/12345
# → Captured: 2026-02-15T14:30:00Z
# → Method: browser-extension (authenticated session)
# → Snapshot: screenshot-abc123.png
# → Cookie profile: "work-twitter" (no credentials stored)

# Find all artifacts from a specific source
ctxt find "source_url=like=x.com/@username*"
ctxt find "capture_method==browser-extension"
```

### Composition Path

#### Investigation Briefs
```bash
# Generate entity dossier
ctxt make brief --entity @person.jane-doe --template osint-profile
ctxt make brief --entity @company.acme-corp --template due-diligence

# Generate timeline report
ctxt make timeline --entity @person.jane-doe --from 2025-06-01 --to 2026-02-01

# Generate cross-reference report
ctxt make report --entities @person.jane-doe,@company.acme-corp --template relationship-map

# Generate change report
ctxt make report --watch x.com/@username --period 30d --template change-summary
```

#### Output Example
```
## Entity Profile: @person.jane-doe

### Identity
- Name: Jane Doe
- Known aliases: @janedoe (X), jane-doe (GitHub), jane.doe (LinkedIn)
- Resolution confidence: 94% (matched on: name, profile photo, employer mention)

### Cross-Platform Presence
| Platform  | Handle/URL                      | Last Captured  | Status   |
|-----------|--------------------------------|----------------|----------|
| X         | x.com/@janedoe                 | 2026-02-15     | Active   |
| GitHub    | github.com/jane-doe            | 2026-02-14     | Active   |
| LinkedIn  | linkedin.com/in/jane-doe       | 2026-02-10     | Active   |
| ArXiv     | arxiv.org/search/?query=...    | 2026-02-12     | 3 papers |

### Activity Timeline
- 2026-02-15: Posted about supply chain security (X)
- 2026-02-14: Pushed commits to vuln-scanner repo (GitHub)
- 2026-02-12: Published paper on dependency confusion (ArXiv)
- 2026-02-10: Updated job title to "Principal Researcher" (LinkedIn)

### Relationships
- @company.acme-corp (employer, confirmed via LinkedIn + GitHub org)
- @person.bob-smith (co-author, confirmed via ArXiv paper)
- @project.vuln-scanner (maintainer, confirmed via GitHub)

### Provenance
- 12 artifacts captured across 4 platforms
- Capture methods: browser-extension (8), cli-capture (4)
- All artifacts have full provenance chain

Generated: 2026-02-18 | Template: osint-profile
```

---

## Key Pain Points

- **Authentication Walls:** Valuable content locked behind logins (LinkedIn, X, paywalled sites); manual copy-paste loses structure and metadata
- **Entity Resolution Ambiguity:** Same name, different people; different handles, same person; no reliable cross-platform identity layer
- **Ephemeral Content:** Tweets get deleted, profiles get edited, pages go offline; if you did not capture it, it is gone
- **Provenance Gaps:** "Where did I find this?" is unanswerable weeks later without systematic metadata tracking
- **Rate Limiting and Detection:** Aggressive scraping triggers CAPTCHAs, IP bans, account restrictions; must respect platform terms
- **Volume Management:** Investigations generate hundreds of artifacts; without structure, the capture folder becomes a graveyard
- **Legal and Ethical Boundaries:** Distinguishing legitimate OSINT from unauthorized access; need clear audit trail for compliance
- **Cross-Format Chaos:** Mix of screenshots, PDFs, HTML snapshots, API responses, manual notes; no unified search across formats

---

## System Leverage

### Browser Extension as Authenticated Capture Bridge
- Extension shares the user's existing session cookies with `ctxt` for capture
- No credentials are stored or transmitted; only session context is used
- User controls which profiles/sessions are available for capture
- Extension captures rendered DOM (not just source), handling JS-heavy SPAs
- Screenshot capture provides archival visual proof alongside structured data
- Domain-specific scraper rules can extract the real article body when generic readability is too noisy

### Cookie-Based Fetching
- CLI `ctxt capture` can use exported cookie files for authenticated fetching
- Supports standard Netscape cookie format (exportable from any browser)
- Cookie freshness monitoring: warns when sessions are about to expire
- Per-platform rate limiting prevents account flagging

### Entity Resolution and Knowledge Graph
- Captured artifacts automatically linked to candidate entities via name, handle, email, URL patterns
- Confidence-scored resolution: high-confidence matches auto-merge, low-confidence flagged for review
- Knowledge graph stores relationships: employer, co-author, collaborator, mentioned-alongside
- Graph traversal enables "who is connected to whom" queries without manual mapping

### Temporal Snapshots and Change Detection
- Every capture creates an immutable snapshot (content + metadata + timestamp)
- `ctxt watch` runs periodic re-captures and computes diffs
- Deletion detection: if content was captured before but is now 404/removed, system flags it
- Historical timeline reconstruction from accumulated snapshots

### Provenance Metadata
- Every artifact stores: source URL, capture timestamp, capture method, authentication context
- Provenance chain is immutable (append-only; snapshots are never overwritten)
- Supports legal defensibility for due diligence and journalism
- Export provenance as structured metadata alongside content

### Platform-Specific Parsers
- Dedicated parsers for major platforms extract structured fields (not just raw HTML)
- X: tweet text, author, timestamp, engagement metrics, thread context, media
- LinkedIn: name, title, company, skills, experience, connections count
- GitHub: repo metadata, contributors, languages, commit activity, issues
- ArXiv: title, authors, abstract, citations, publication date, categories
- Parsers handle platform HTML/API changes via versioned extraction rules

---

## User Stories

Researchers and OSINT analysts interact with the system through these key stories:

### OSINT-Specific Capture
- [US-0200](../stories/capture/US-0200-browser-cookie-bridge.md) — Browser Cookie Bridge (authenticated capture without storing credentials)
- [US-0201](../stories/capture/US-0201-x-twitter-capture.md) — X/Twitter Capture (profile + tweet extraction)
- [US-0202](../stories/capture/US-0202-github-capture.md) — GitHub Capture (repo metadata + activity)
- [US-0203](../stories/capture/US-0203-arxiv-capture.md) — ArXiv Capture (paper metadata + authors)
- [US-0204](../stories/capture/US-0204-linkedin-capture.md) — LinkedIn Capture (profile extraction, requires auth)
- [US-0205](../stories/capture/US-0205-wikipedia-capture.md) — Wikipedia Capture (article + citations)
- [US-0206](../stories/capture/US-0206-osint-entity-aggregation.md) — OSINT Entity Aggregation (cross-platform profile building)
- [US-0207](../stories/capture/US-0207-web-tab-capture.md) — Web Tab Capture (browser extension integration)
- [US-0208](../stories/capture/US-0208-temporal-watch.md) — Temporal Watch (change detection over time)
- [US-0209](../stories/capture/US-0209-authenticated-web-fetch.md) — Authenticated Web Fetch (cookie-based fetching)
- [US-0210](../stories/capture/US-0210-cross-platform-entity-resolution.md) — Cross-Platform Entity Resolution (identity matching)

### Capture & Ingestion
- [US-0001](../stories/ingestion/US-0001-text-capture-minimal-friction.md) -- Text Capture with Minimal Friction (quick notes during investigation)
- [US-0002](../stories/ingestion/US-0002-url-capture-and-extraction.md) -- URL Capture and Extraction (capture web pages with structure)
- [US-0003](../stories/ingestion/US-0003-image-ocr-and-analysis.md) -- Image OCR and Analysis (screenshots, diagrams, scanned documents)
- [US-0006](../stories/ingestion/US-0006-document-parsing-and-decomposition.md) -- Document Parsing and Decomposition (PDFs, reports, filings)
- [US-0007](../stories/ingestion/US-0007-feed-ingestion-and-sync.md) -- Feed Ingestion and Sync (RSS/Atom monitoring for ongoing topics)
- [US-0008](../stories/ingestion/US-0008-batch-import-from-file.md) -- Batch Import from File (bulk URL list ingestion)

### Enrichment & Understanding
- [US-0009](../stories/enrichment/US-0009-extract-entities-and-mentions.md) -- Extract Entities and Mentions (auto-identify people, orgs, locations)
- [US-0046](../stories/enrichment/US-0046-extract-relationships-between-entities.md) -- Extract Relationships Between Entities (co-authorship, employment, affiliation)
- [US-0047](../stories/enrichment/US-0047-extract-temporal-information.md) -- Extract Temporal Information (dates, timelines, event sequencing)
- [US-0048](../stories/enrichment/US-0048-detect-sentiment-and-tone.md) -- Detect Sentiment and Tone (assess source credibility and bias)
- [US-0049](../stories/enrichment/US-0049-classify-content-with-taxonomy.md) -- Classify Content with Taxonomy (categorize by investigation domain)

### Search & Discovery
- [US-0016](../stories/search/US-0016-natural-language-search.md) -- Natural Language Search (search across all captured OSINT)
- [US-0051](../stories/search/US-0051-semantic-search-with-embeddings.md) -- Semantic Search with Embeddings (find conceptually related artifacts)
- [US-0052](../stories/search/US-0052-graph-based-entity-search.md) -- Graph-Based Entity Search (traverse relationships)
- [US-0054](../stories/search/US-0054-saved-search-and-alerts.md) -- Saved Search and Alerts (ongoing monitoring)
- [US-0061](../stories/search/US-0061-multimodal-search.md) -- Multimodal Search (search across text, images, documents)

### Composition & Reporting
- [US-0022](../stories/composition/US-0022-generate-brief-from-objects.md) -- Generate Brief from Objects (investigation summaries)
- [US-0025](../stories/composition/US-0025-export-brief-to-markdown-pdf.md) -- Export Brief to Markdown/PDF (shareable reports)
- [US-0056](../stories/composition/US-0056-compose-decision-timeline.md) -- Compose Decision Timeline (event chronology)
- [US-0057](../stories/composition/US-0057-compose-stakeholder-analysis.md) -- Compose Stakeholder Analysis (who is involved and how)
- [US-0060](../stories/composition/US-0060-compose-with-custom-template.md) -- Compose with Custom Template (investigation-specific formats)

---

## Success Metrics

- **Capture friction:** Time from "see interesting content" to "searchable in knowledge base" < 5 seconds (via browser extension)
- **Cross-platform entity match rate:** > 80% for well-known entities with multiple platform presences
- **Zero data loss:** Captured snapshots preserved regardless of source content deletion or editing
- **Full provenance chain:** 100% of artifacts have source URL, capture time, and authentication method recorded
- **Change detection latency:** Time from source change to alert < 2x watch interval (e.g., < 48h for 24h interval)
- **Entity resolution precision:** < 5% false positive merge rate (wrong entities linked together)
- **Investigation throughput:** Artifacts captured per hour during active research > 50 (vs. ~10 with manual methods)
- **Report generation:** Time from "generate dossier" to "exportable brief" < 30 seconds for entities with < 100 artifacts

---

## Collaboration with Other Personas

- **Knowledge Workers:** Researchers share captured content via briefs and compositions; knowledge workers consume investigation outputs as context for decisions
- **Agents/LLMs:** Automated entity resolution and cross-referencing; agents run periodic enrichment on captured artifacts; constrained extraction ensures structured entity profiles
- **Maintainers:** Researchers request platform-specific parsers, entity resolution improvements, and new capture integrations
- **Platform Integrators:** Integrators build browser extensions, platform connectors, and export adapters that researchers depend on
- **Operations:** Operations monitors capture job health, cookie freshness alerts, rate limiting compliance, and storage growth from high-volume investigations

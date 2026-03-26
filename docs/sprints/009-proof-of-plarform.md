# Skeleton 9 Goal: "Proof of Platform"

## Package Focus

**Primary Package:** dPKMS (60%) + ctxt (40%)

Final validation requires stress testing infrastructure and proving external integrations work.

**Package Breakdown:**
- **dPKMS:** Stress testing, housekeeping, entity/graph validation, migration stability
- **ctxt:** Browser extension, Raycast script, prompt evaluation, model agnosticism

---

By the end of this skeleton:

1. **Reference Integrations:** An official **Browser Extension** and **Raycast Script** are released, proving the API works end-to-end, including **mention-aware ingestion**, **entity resolution**, and **graph updates**.
2. **Stress Tested:** The system is verified to handle **100,000+ knowledge objects**, **millions of mentions**, large **multiregistry entity sets**, and dense **knowledge graph edges** without UI or API lag.
3. **Documentation:** The `docs/` folder is converted into a user-facing static website, now including the new **Mentions**, **Entity Schema**, and **Knowledge Graph** docs, with end-to-end examples and query demonstrations.

---

## 1. dPKMS Infrastructure Team (The Stress Testers)

**Focus:** Reliability, Graph Integrity, and Long-Running Stability.
**Why:** Users leave `dpkms serve` running for weeks. Memory leaks, unstable entity indexes, or graph inconsistencies will break trust.

### Task 1.1: The "Million Knowledge Object + Entity Graph" Simulation

- Create a test harness that ingests:
  - 100k synthetic text records
  - 10k images
  - **simulated mentions referencing ~5k synthetic entities across multiple namespaces**
- Monitor:
  - steady-state RAM usage (<500MB target)
  - SQLite WAL file growth characteristics
  - FTS indexing throughput during ingestion
  - **entity index growth**
  - **backlinks table density and fragmentation**
- Action: Tune SQLite pragmas (`mmap_size`, `cache_size`, `wal_autocheckpoint`, `page_size`) based on results.

### Task 1.2: `dpkms housekeeping` Command

Implement a maintenance command.

Actions:

- `VACUUM` and `wal_checkpoint` the SQLite DB
- Prune `jobs` history >30 days
- Rotate logs
- **Rebuild or compact backlinks and entity index when fragmentation exceeds thresholds**
- Validate integrity of:
  - dangling entity references
  - orphaned mentions
  - inconsistent graph edges

### Task 1.3: The "One-Line" Installer

Finalize `install.sh`.

Logic:

- Detect OS and architecture
- Fetch binary from GitHub Releases
- Validate checksum
- Install to `$PATH`
- Setup `systemd` or `launchd` service
- **Initialize local entity registry**
- **Create graph tables (entity index, backlinks)**
- Ensure migrations run automatically on first launch

---

## 2. ctxt Ingestion Team (The Tuner)

**Focus:** Quality of intelligence and correctness of semantic identity.
**Why:** Pipelines must produce stable semantic references (mentions → entities → graph edges).

### Task 2.1: Prompt Eval Suite

- Build a test set of 50 complex inputs across modalities.
- Validate:
  - summaries
  - tags
  - **mention extraction correctness**
  - **entity resolution behavior (local vs registry)**
  - absence of hallucinated entities
- Measure stability of mentions across model providers.

### Task 2.2: Deduplication Logic (`text.exact`)

- Use SHA256 content hashing to detect duplicates.
- Logic:
  - If duplicate detected, **merge mentions into existing bookmark**
  - Update backlinks accordingly
  - Preserve provenance chain

### Task 2.3: Model Agnosticism Check

- Ensure `text.short` and mention extractor behave consistently across models.
- Validate with:
  - `gpt-4o`
  - `llama3`
  - optional OSS models
- Add config presets for each.
- Verify stability of:
  - **mention syntax**
  - **entity ID mapping**
  - **avoiding accidental new entity creation**

---

## 3c. Infrastructure / Security Hardening

### Task 3c.3: Static Security Analysis with gosec

Add `gosec` to CI and lint pipeline; fail on medium+ severity.

**Artifacts:**
- `.gosec.yaml` — project-specific rule exclusions and per-path overrides
- `.golangci.yml` `linters-settings.gosec` — references `.gosec.yaml`; severity: medium
- `Makefile` `gosec` target — standalone `gosec -severity medium` scan; `lint` depends on it
- `.github/workflows/ci.yml` `security` job — dedicated gosec step; SARIF uploaded to GitHub
  Security tab; fails PR on any medium+ finding

**Excluded rules (with rationale):**
- `G104` — duplicate of errcheck; avoid double-reporting
- `G304` — file-path from variable required for config/import paths; callers validate
- `G307` — defer-on-error-returning-method is well-known; handled at close site

**Test file relaxations:**
- `G101` severity lowered to `low` in `_test.go` — test fixtures contain intentional
  credential-like strings

---

## 3. dPKMS Search Team & ctxt Retrieval Team (The Integrator)

**Focus:** Consuming the API for real tools.
**Why:** This sprint proves that mention- and entity-aware APIs are usable and performant.

### Task 3.1: The Chrome Extension (MVP)

Build minimal JS extension.

Function:

- Click icon → Capture tab URL/selection → `POST /analyze`
- Extension UI shows:
  - detected mentions
  - resolved entities
  - brief semantic summary
- Validate:
  - CORS headers
  - mention-aware ingestion
  - entity metadata returned from the API

### Task 3.2: The Raycast/Alfred Script

Write bash/python script using `curl`.

Function:

- "Search Context" command
- Calls:
  - `GET /bookmarks?q=mention:ui.*`
  - `GET /bookmarks?q=mention:stripe.api.checkout`
- Copies Markdown link to clipboard

Validation:

- mention-based queries under 200ms
- graph-aware ranking does not regress performance
- entity → knowledge_object → entity traversal paths remain stable

### Task 3c.2: Dependency Vulnerability Scanning (T-0147)

Add `govulncheck` + `nancy` to CI; block merges on CRITICAL/HIGH findings.

**Tools:**
- `govulncheck` — official Go vuln scanner; call-graph-aware (vuln.go.dev).
  Only reports reachable vulnerabilities → low false-positive rate.
- `nancy` — Sonatype OSS Index scanner; broader CVE coverage; pipe
  `go list -json -deps ./...` output to `nancy sleuth`.

**Triggers:**
- Every PR targeting `main` / `develop`
- Nightly cron (`0 3 * * *` UTC)

**Artifacts:**
- `.github/workflows/vuln-scan.yml` — dedicated workflow (2 jobs: `govulncheck`,
  `nancy`); both jobs required for merge.
- `Makefile` target `vuln-scan` — local dev invocation (`make vuln-scan`).
- `.nancy-ignore` — suppression file; process documented in file header.

**Suppression process (summary):**
1. Run `make vuln-scan`; note finding ID.
2. Open tracking issue; confirm reachable code path.
3. Prefer upgrading dep over suppressing.
4. If suppression unavoidable: add ID to `.nancy-ignore` with date, issue URL,
   rationale, 90-day review-by date.
5. CRITICAL (CVSS ≥ 9.0) or HIGH (CVSS ≥ 7.0) suppressions need maintainer
   sign-off in the tracking issue.

---

## 4. dPKMS Registry Team (The Teacher)

**Focus:** Developer Portal, Documentation, and Registry Usability.
**Why:** Mentions, entities, and graph semantics must be documented thoroughly and consistently.

### Task 4.1: Static Doc Site

Deploy a Hugo/Docusaurus site.

Content includes:

- Installation guide
- Getting Started
- **mentions.md**
- **schema-entity.md**
- **knowledge-graph.md**
- Registry Protocol
- Plugin guides
- Example registries providing entities
- Entity lifecycle and versioning
- Query examples using `mention:`

### Task 4.2: Error Code Catalog

Standardize error messages.

Examples:

- `CH_ERR_ENTITY_NOT_FOUND`
- `CH_ERR_MENTION_SYNTAX_INVALID`
- `CH_ERR_ENTITY_ALIAS_CONFLICT`
- `CH_ERR_GRAPH_CORRUPT`

Terminal output links directly to the docs.

### Task 4.3: Community Registry Submission Flow

Create GitHub Issue template:

- Taxonomy registries
- **Entity registries (canonical concepts, APIs, ontologies)**
- Mixed registries
- Experimental registries

Provide guidance for:

- namespacing
- versioning
- alias management
- translation support

---

## The Integration Check (The Demo)

Scenario: The "New User" Experience with Semantic Identity Enabled.

1. **Installation:**
   A clean VM runs:
   ```
   curl ... | bash
   ```
   Check:
   - service starts
   - **entity tables initialized**
   - graph indexes exist

2. **Integration:**
   User installs the Chrome Extension.

3. **Usage:**
   User browses 50 pages, clicking "Save to Context".
   Check:
   - background worker processes jobs
   - **mentions extracted from text, URL, metadata**
   - **entities resolved or created as local entities**
   - **graph edges generated (knowledge_object → entity)**

4. **Retrieval:**
   User opens Raycast, types:
   ```
   context: @ui.best-practice
   ```
   Check:
   - mention-based search returns correct bookmarks
   - entity metadata included
   - responses <200ms

5. **Maintenance:**
   Overnight run.
   Check:
   - stable RAM
   - no WAL ballooning
   - graph queries remain below latency thresholds
   - `dpkms housekeeping` compacts entity index and backlinks

---

## Risks to Watch For

- **WAL Growth:** Backlink writes are frequent; WAL tuning required.
- **Entity Explosion:** Unchecked mention extraction may create unnecessary local entities.
- **Graph Corruption:** Partial ingestion must not create orphaned edges.
- **Registry Alias Conflicts:** Multi-source registries may define overlapping entities.
- **CORS Requirements:** Browser extension must handle entity/mention payloads.
- **Documentation Drift:** Entity and mention schemas must remain synchronized.

---

## Post-Sprint 9: Version 1.0.0 Release

After this skeleton, the system is:

1. **Feature Complete**
   Multimodal ingestion, mentions, entities, knowledge graph, query language, registry sync.

2. **Stable**
   Migrations validated, graph integrity verified, long-running workers tested.

3. **Extensible**
   Plugins, registries, pipelines, custom entity providers.

4. **Documented**
   Full static site launched with semantic identity and graph documentation.
---

## Task 3c.1: Pre-Commit Secret Scanning (gitleaks)

Protect repository from accidental credential leaks.

### Implementation

- **`.gitleaks.toml`** — custom rules + allow-list:
  - standard patterns: OpenAI, Anthropic, AWS, GitHub, Slack, JWT, PG connection strings
  - CTXT-specific rules: `CTXT_API_KEY`, `CTXT_SECRET/TOKEN/PASSWORD/SIGNING_KEY`,
    `CTXT_DB_*`, `CTXT_[PROVIDER]_API_KEY`
  - allow-list: `.env.example`, `config.example.yaml`, `testdata/`, `docs/`, placeholder values,
    `${VAR}` references, CTXT_* vars set to empty/placeholder
  - extends gitleaks default ruleset (`useDefault = true`)

- **`make security-scan`** — runs `gitleaks detect --source . --config .gitleaks.toml`;
  fails CI-style if gitleaks not installed

- **`make install-hooks`** — writes `.git/hooks/pre-commit` running
  `gitleaks protect --staged --config .gitleaks.toml`; soft-fail with warning if gitleaks absent

- **CI (`secret-scan` job)** — uses `gitleaks/gitleaks-action@v2` on every push + PR;
  `fetch-depth: 0` for full history scan; gates `build` job alongside `lint`, `security`, `test`

### Usage

```
# Local setup (one-time)
brew install gitleaks
make install-hooks

# Manual full-history scan
make security-scan

# CI: automatic on every push/PR via .github/workflows/ci.yml
```

---

## See Also

**Package Boundaries:**
- [CROSS-PACKAGE-CONTRACTS.md](CROSS-PACKAGE-CONTRACTS.md) - dPKMS ↔ ctxt integration points
- [../branding.md](../branding.md) - Naming conventions (dPKMS vs ctxt vs ContextHelp)
- [../dpkms-or-ctxt.md](../dpkms-or-ctxt.md) - Package placement guide

**Configuration:**
- [CONFIGURATION-STRUCTURE.md](CONFIGURATION-STRUCTURE.md) - Config file organization
- [../ctxt/configuration.md](../ctxt/configuration.md) - Focus profiles & preferences

**Architecture:**
- [../architecture.md](../architecture.md) - System architecture overview
- [../../ROADMAP.md](../../ROADMAP.md) - Living skeleton roadmap
- [README.md](README.md) - Sprint documentation index

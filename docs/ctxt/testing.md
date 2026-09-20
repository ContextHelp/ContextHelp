# Testing (`ctxt` Brain)

This document outlines the testing strategy for **`ctxt`** — the agentic brain layer providing capture, enrichment, surfacing, and composition.

`ctxt` testing focuses on **behavioral correctness**: enrichment quality, user experience, and intelligent surfacing.

---

## Philosophy

Testing `ctxt` follows four core principles:

- **Behavioral:** Tests validate user-facing behavior, not implementation details
- **Deterministic:** Given identical inputs, enrichment outputs must be stable
- **Mockable:** AI providers, registries, and dPKMS must be mockable
- **User-Centric:** Tests reflect real usage patterns and workflows

---

## Test Categories

### Unit Tests

Small, fast tests targeting individual brain components:

**Capture Layer:**
- Input normalization (URLs, text, files)
- Type inference logic
- Profile context application
- Offline queue behavior

**Enrichment Recipes:**
- Pipeline step execution (text.short, text.long)
- Mention extraction from text
- Tag assignment logic
- Decision extraction
- Task extraction
- Summary generation

**Focus Profiles:**
- Profile configuration loading
- Scoping rules (entity filters, tag boosts)
- Ranking weight adjustments
- Template selection

**Composition Engine:**
- Template rendering
- Atomic node assembly
- Provenance attachment
- Output formatting

**CLI Commands:**
- Argument parsing
- Output formatting
- Error handling
- Help text generation

Unit tests **use mocked dPKMS and AI providers**.

---

### Integration Tests

Integration tests verify full brain subsystems:

**Capture → Enrichment:**
- `ctxt add <url>` → job enqueued → enrichment runs → object created
- Pipeline selection based on type
- Profile-specific pipeline overrides
- Background enrichment completion

**Search → Retrieval:**
- `ctxt find <query>` → dPKMS query → profile filtering → ranked results
- Fuzzy search behavior
- Entity-aware lookup
- Profile boost application

**Composition Workflows:**
- `ctxt make brief` → gather context → apply template → generate output
- Provenance traceability
- Graph-aware assembly
- Profile-specific templates

**Profile Switching:**
- `ctxt profile use engineer` → behavior changes
- Registry scoping
- Ranking adjustments
- Surfacing rules

Integration tests use **isolated dPKMS instances** and **mocked AI providers**.

---

### End-to-End Tests

E2E tests simulate real daily usage:

**Daily Workflow:**
```bash
# Capture during meeting
ctxt add https://example.com/competitor-teardown

# Search later
ctxt find "usage pricing retention"

# View structured output
ctxt open <id>

# Generate brief
ctxt make brief --from <id>

# Switch to focused mode
ctxt profile use founder

# Check what matters now
ctxt agenda --tomorrow
```

**Multimodal Capture:**
- Text input → enrichment → retrieval
- URL → fetch → clean → analyze → retrieval
- Image → OCR → entity extraction → retrieval
- File upload → processing → storage

**Progressive Enrichment:**
- Raw capture → minimal processing
- Background enrichment → full structure
- Re-enrichment on demand

E2E tests validate **complete user journeys**.

---

## Test Fixtures

Fixtures live in `ctxt/testdata/`:

**Inputs:**
- `inputs/text-short.txt` - Short text samples
- `inputs/text-long.md` - Long articles
- `inputs/urls.txt` - Test URLs
- `inputs/images/*.png` - Test images
- `inputs/documents/*.pdf` - Test PDFs

**Enrichment:**
- `enrichment/summaries.json` - Expected summaries
- `enrichment/entities.json` - Expected entity extractions
- `enrichment/tags.json` - Expected tag assignments
- `enrichment/decisions.json` - Expected decision extractions

**Profiles:**
- `profiles/founder.yaml` - Founder profile config
- `profiles/engineer.yaml` - Engineer profile config
- `profiles/research.yaml` - Research profile config

**Compositions:**
- `compositions/brief-template.md` - Brief template
- `compositions/plan-template.md` - Plan template
- `compositions/expected-outputs/` - Golden outputs

**CLI:**
- `cli/commands.txt` - Test CLI invocations
- `cli/expected-output/` - Expected CLI outputs (snapshot testing)

---

## Mocking Strategy

### Mock AI Providers

Replace real AI calls with deterministic responses:

**Mock LLM:**
```go
type MockLLM struct {
    responses map[string]string
}

func (m *MockLLM) Generate(prompt string) string {
    return m.responses[promptHash(prompt)]
}
```

**Mock Embeddings:**
```go
type MockEmbeddings struct {
    vectors map[string][]float32
}

func (m *MockEmbeddings) Embed(text string) []float32 {
    return m.vectors[textHash(text)]
}
```

### Mock dPKMS

Use in-memory dPKMS implementation:
- Fast test execution
- Deterministic behavior
- Full substrate API compliance
- No external dependencies

### Mock External Services

For URL pipelines:
- Mock HTTP client
- Cached HTML responses
- Deterministic content extraction

---

## Critical Test Scenarios

### Capture Without Classification

```go
// Test: User captures without thinking
1. User runs: ctxt add "random thought"
2. Assert: Object created immediately
3. Assert: Job queued for enrichment
4. Assert: User gets instant confirmation
5. Background: Job runs, object enriched
6. Assert: Object now has tags, mentions, summary
```

### Search Under Uncertainty

```go
// Test: Fuzzy search finds results
1. Create object with title "authentication flow"
2. User searches: ctxt find "authen flw"  // typos
3. Assert: Object found via fuzzy matching
4. Assert: Results ranked by relevance
```

### Profile-Aware Enrichment

```go
// Test: Profile changes pipeline selection
1. Set profile: ctxt profile use engineer
2. Add code snippet
3. Assert: code.snippet pipeline selected (not text.short)
4. Assert: Technical tags assigned
5. Set profile: ctxt profile use founder
6. Add same content
7. Assert: Business-focused tags assigned instead
```

### Composition Traceability

```go
// Test: Compositions link back to sources
1. Create objects A, B, C with entities
2. Run: ctxt make brief --from A,B,C
3. Assert: Brief generated
4. Assert: Brief metadata includes source IDs
5. Assert: Mentions in brief link to entities
6. User can trace every claim back to source
```

### Just-In-Time Surfacing

```go
// Test: Relevant knowledge resurfaces
1. Create project-related objects (tagged "Project X")
2. Switch profile: ctxt profile use "project-x"
3. Run: ctxt agenda
4. Assert: Related objects surface
5. Assert: Recent activity prioritized
6. Assert: Entity-connected items included
```

---

## Enrichment Quality Testing

### Summary Quality

Tests validate:
- Summaries capture key points
- Summaries are concise (target length)
- Summaries preserve entities
- Summaries handle multiple languages

### Entity Extraction

Tests validate:
- Mentions extracted correctly (`@entity.slug`)
- Entity candidates reasonable
- False positives minimized
- Context-appropriate extraction

### Tag Assignment

Tests validate:
- Tags match content semantically
- Tag weights reasonable
- Taxonomy alignment
- No irrelevant tags

### Decision Extraction

Tests validate:
- Actual decisions identified
- Rationales captured
- Timestamps preserved
- Follow-ups linked

---

## CLI Testing

### Command Testing

Validate each CLI command:

**ctxt add:**
```bash
# Test various input types
ctxt add "text input"
ctxt add --file document.pdf
ctxt add https://example.com
ctxt add --stdin < input.txt
```

**ctxt find:**
```bash
# Test search variations
ctxt find "keyword"
ctxt find --type article --tag design
ctxt find mention:@stripe.api
ctxt find similar:"error handling patterns"
```

**ctxt open:**
```bash
# Test object display
ctxt open <id>
ctxt open <id> --format json
ctxt open <id> --show-graph
```

**ctxt make:**
```bash
# Test composition
ctxt make brief --from <ids>
ctxt make plan --topic "authentication"
ctxt make checklist --from <id>
```

**ctxt profile:**
```bash
# Test profile management
ctxt profile list
ctxt profile use engineer
ctxt profile show
```

### Output Formatting

Snapshot tests for:
- Table formatting
- JSON output
- Markdown rendering
- Color codes (when TTY)
- Plain text (when piped)

### Error Messages

Tests validate:
- Clear error descriptions
- Actionable suggestions
- No stack traces to user
- Appropriate exit codes

---

## Integration Quality Tests

### Multilingual Handling

Tests for polyglot behavior:
```go
// Test: Mixed language content
1. Add content with Arabic + English
2. Assert: Both languages indexed
3. Assert: Entity extraction works across languages
4. Assert: Search finds content in either language
```

### Offline Behavior

Tests for offline-first:
```go
// Test: Offline capture
1. Disconnect network
2. Run: ctxt add "offline content"
3. Assert: Content saved to local queue
4. Assert: User gets confirmation
5. Reconnect network
6. Assert: Background sync completes
```

---

## Performance Testing

`ctxt` must feel instant:

**Capture Performance:**
- `ctxt add` returns in <100ms
- Local queue write <50ms
- Enrichment runs in background

**Search Performance:**
- `ctxt find` returns in <500ms
- Profile filtering adds <50ms overhead
- Fuzzy matching acceptable latency

**Composition Performance:**
- `ctxt make brief` completes in <5s
- Graph assembly <1s
- Template rendering <500ms

---

## User Experience Testing

### Frictionless Capture

Manual tests:
- Can user capture without thinking?
- No classification required?
- Instant feedback?
- No errors on "messy" input?

### Forgiving Search

Manual tests:
- Search works with typos?
- Partial matches useful?
- Entity navigation intuitive?
- Results ranked sensibly?

### Actionable Composition

Manual tests:
- Briefs traceable to sources?
- Templates match use case?
- Outputs publish-ready?
- Provenance clear?

---

## Continuous Integration

CI for `ctxt`:
- **Unit tests:** Every commit
- **Integration tests:** Every PR
- **E2E tests:** Main branch merges
- **CLI snapshot tests:** Every PR
- **Enrichment quality:** Weekly validation

**Checks:**
- Mock AI providers only (no real API calls)
- CLI output snapshots match
- Profile behavior correct
- No regressions in enrichment quality

---

## Test Coverage Requirements

**Minimum coverage by component:**
- Capture layer: 85%
- Enrichment recipes: 80%
- Focus profiles: 85%
- Composition engine: 80%
- CLI commands: 90%

---

## Manual Testing Checklist

Before release, manually test:

- [ ] Daily workflow (capture → search → open → compose)
- [ ] Profile switching changes behavior
- [ ] Offline mode works
- [ ] Multilingual content handled
- [ ] All CLI commands work
- [ ] Error messages helpful
- [ ] Output formatting clean
- [ ] Composition quality acceptable

---

## Recall Harness (hop.top/ben)

ADR-070 §6 (pipeline-version gate) and ADR-071 Phase 3 (embedding-migration gate) both require a deterministic recall benchmark on a fixed corpus + query list. We adopt **`hop.top/ben`** for this, mirroring the `hop.top/xrr` cassette pattern (`internal/adapter/{mic,feeds/rss,screen,files/s3}`).

### Where things live

| File | Purpose |
|------|---------|
| `suites/recall-text-short.ben.yaml` | Gates `reingest_selective` PRs (text.short pipeline) |
| `suites/recall-vector.ben.yaml` | Gates `ctxt embeddings migrate` (ADR-071 Phase 3) |
| `test/integration/testdata/ben-fixtures/text-short-corpus.yaml` | 24 short-form objects covering bullet+hyphen edge cases |
| `test/integration/testdata/ben-fixtures/text-short-queries.yaml` | 23 queries with explicit `expected_ids` mapping |
| `test/integration/testdata/ben-fixtures/vector-corpus.yaml` | 20 paraphrase-friendly objects (semantic-leaning) |
| `test/integration/testdata/ben-fixtures/vector-queries.yaml` | 10 paraphrase queries; lexical baseline scores ~0.75 today |
| `cmd/ben-adapter-ctxt-recall/` | The hop.top/ben binary plugin that scores recall@k |
| `scripts/ben-floor.sh` | Reads `ben run --format json` output and enforces the recall floor |
| `.github/workflows/ben.yml` | CI gate triggered by retrieval-substrate changes (currently disabled — see "Known limitations") |

### Running locally

```sh
# Run both suites and enforce floors:
make ben

# Run just one:
make ben-text-short
make ben-vector
```

`hop.top/ben` is consumed via local-path replace — no published version
exists yet. The Makefile resolves it in this order:

`$BEN_LOCAL_PATH` must point at a ben checkout. There is no default path,
since the location depends on how you arrange your checkouts.

If it is unset or does not resolve, `make ben` fails loudly with a
remediation hint.

### Interpreting a recall-floor failure

A failing `make ben-*` looks like:

```
[FAIL] current      recall_at_k=0.72  (floor=0.85)
ben-floor: at least one candidate dropped below the floor; see ADR-070 §6 for the escape-hatch process.
```

Three diagnoses, in order of likelihood:

1. **Real regression.** The PR introduced a tokenizer / pipeline / index
   change that genuinely lost recall on previously-retrieved objects.
   Fix the regression and re-run; the gate should clear.
2. **Fixture drift.** The PR removed or renamed an object the queries
   reference, but didn't update the queries. Edit the queries fixture
   alongside the corpus change.
3. **Legitimate trade-off** (e.g. tokenizer change improves precision at
   a small cost to recall, see ADR-070's escape-hatch language). This is
   the only case where lowering the floor in the suite YAML is correct.
   See "Updating a suite" below.

### Updating a suite (the ADR-070 escape-hatch path)

Lowering a recall floor or removing a query is an operator-impacting
change. The PR doing so must:

1. Edit `suites/<name>.ben.yaml` and/or the floor in the Makefile
   (`BEN_TEXT_SHORT_FLOOR` / `BEN_VECTOR_FLOOR`).
2. Add a `reingest_selective` row to `docs/release-notes/<date>.md`
   per ADR-070 §4 — the release-notes-check workflow will fail otherwise.
3. Carry an `Operator-Impact: reingest_selective` trailer on the commit
   that lowers the floor.

The release-notes row must explain why the recall trade-off is
acceptable; reviewers should treat a recall-floor drop as a yellow flag
that needs justification, not a routine bump.

### Authoring a new fixture

Keep both fixtures small. Targets:

- `text-short-corpus.yaml`: < ~50 objects so the harness clears in under
  a second on a laptop.
- `vector-corpus.yaml`: < ~30 objects until the real vector leg ships
  (T-0584); after that the cassette set is the binding constraint.

Each query must have at least one `expected_id` present in the matching
corpus file — the adapter validates this and exits 1 otherwise. Multiple
`expected_ids` is fine and exercises AND-style relevance.

### Known limitations

- **CI gate is disabled** (`if: false` on the job in
  `.github/workflows/ben.yml`). ben is consumed via local-path replace
  and has no published tag, so a clean CI runner can't resolve it
  without checking out ben adjacent to ctxt. Re-enable once ben
  publishes a tagged version (gated on the coordinated open-source
  reset to `0.1.0-alpha.0` for every kit-powered package — see T-0196).
  At that point the Makefile gains `go install hop.top/ben/cmd/ben@<tag>`
  as a third resolution tier and CI works without sibling checkout.
- **Vector leg is a lexical baseline today.** Both candidates in
  `recall-vector.ben.yaml` run the lexical baseline. T-0584 wires the
  candidate-model leg through xrr cassettes; the suite shape is in
  place so that PR is purely additive.

---

## Summary

`ctxt` testing ensures:
- **Frictionless capture** - Zero resistance to input
- **Forgiving search** - Finds content under uncertainty
- **Quality enrichment** - Summaries, tags, entities accurate
- **Actionable output** - Compositions useful and traceable
- **Consistent UX** - Behavior matches user expectations

See also:
- [../dpkms/testing.md](../dpkms/testing.md) - Substrate layer testing
- [pipelines.md](pipelines.md) - Enrichment recipes
- [api-cli.md](api-cli.md) - CLI reference
- [configuration.md](configuration.md) - Focus profiles

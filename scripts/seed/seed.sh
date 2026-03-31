#!/usr/bin/env bash
# scripts/seed/seed.sh
#
# Intelligent seed script for ContextHelp — exercises all major features.
#
# Sources:
#   - Pinboard bookmarks   (PINBOARD_TOKEN)
#   - GitHub starred/watched repos (GITHUB_TOKEN)
#   - Synthetic ctxt analyze items (inline, covers edge cases)
#   - RSS feeds
#
# TODO (when credentials available):
#   - Twitter/X: ctxt import twitter --file /path/to/tweets.js --username jadb
#   - Google Drive: ctxt import gdrive --access-token $GDRIVE_ACCESS_TOKEN --max-items 100
#
# Usage:
#   PINBOARD_TOKEN=username:hextoken \
#   GITHUB_TOKEN=gho_... \
#   bash scripts/seed/seed.sh
#
# The script is idempotent — re-running it will not duplicate objects.

set -euo pipefail

CTXT="${CTXT:-ctxt}"
SERVER="${CTXT_SERVER:-http://localhost:8080}"
PINBOARD_TOKEN="${PINBOARD_TOKEN:?ERROR: PINBOARD_TOKEN is required (format: username:hextoken)}"
GITHUB_TOKEN="${GITHUB_TOKEN:-}"
GITHUB_USER="${GITHUB_USER:-jadb}"

log() { echo "==> $*" >&2; }
section() { echo; echo "━━━ $* ━━━"; echo; }

# ─────────────────────────────────────────────
# 0. Pre-flight
# ─────────────────────────────────────────────
section "Pre-flight checks"

if ! $CTXT --version &>/dev/null; then
  echo "ERROR: ctxt not found. Add it to PATH or set CTXT=/path/to/ctxt" >&2
  exit 1
fi

log "ctxt: $($CTXT --version 2>&1 | head -1)"
log "server: $SERVER"

# ─────────────────────────────────────────────
# 1. Profiles
# ─────────────────────────────────────────────
section "1. Focus profiles"

for profile in founder engineer researcher; do
  if $CTXT profile show "$profile" &>/dev/null; then
    log "profile '$profile' already exists — skipping"
  else
    log "creating profile: $profile"
    $CTXT profile create "$profile"
  fi
done

log "setting default profile: founder"
$CTXT profile set-default founder

# ─────────────────────────────────────────────
# 2. Registries
# ─────────────────────────────────────────────
section "2. Registry subscriptions"

log "listing existing registries"
$CTXT registry list || true

# ─────────────────────────────────────────────
# 3. RSS feeds
# ─────────────────────────────────────────────
section "3. RSS feeds"

feeds=(
  "https://changelog.com/gotime/feed"
  "https://feeds.feedburner.com/ThoughtWorksTechnology"
  "https://www.indiehackers.com/feed.xml"
  "https://news.ycombinator.com/rss"
  "https://martinfowler.com/feed.atom"
  "https://go.dev/blog/feed.atom"
  "https://blog.pragmaticengineer.com/rss/"
  "https://lethain.com/feeds/"
  # GitHub Trending (unofficial Atom feeds — best-effort, may break)
  "https://github.com/trending/go.atom"
  "https://github.com/trending/typescript.atom"
  "https://github.com/trending/python.atom"
  "https://github.com/trending/php.atom"
)

for feed in "${feeds[@]}"; do
  log "adding feed: $feed"
  $CTXT feed add "$feed" --server "$SERVER" || log "  (already subscribed or unavailable)"
done

# ─────────────────────────────────────────────
# 4. Import: Pinboard
# ─────────────────────────────────────────────
section "4. Pinboard import"

log "importing all Pinboard bookmarks"
$CTXT import pinboard \
  --token "$PINBOARD_TOKEN" \
  --server "$SERVER"

# ─────────────────────────────────────────────
# 5. Import: GitHub
# ─────────────────────────────────────────────
section "5. GitHub import"

GITHUB_FLAGS="--username $GITHUB_USER --server $SERVER --lists starred,watched"
if [[ -n "$GITHUB_TOKEN" ]]; then
  GITHUB_FLAGS="$GITHUB_FLAGS --token $GITHUB_TOKEN"
fi

log "importing GitHub starred + watched repos for $GITHUB_USER"
# shellcheck disable=SC2086
$CTXT import github $GITHUB_FLAGS

# ─────────────────────────────────────────────
# 6. Synthetic items — text captures
# ─────────────────────────────────────────────
section "6. Synthetic text captures"

analyze() {
  local content="$1"; shift
  log "analyze: ${content:0:60}..."
  $CTXT analyze "$content" --server "$SERVER" "$@" || true
}

# Founder / strategy
analyze "We need to decide: build vs buy for our auth layer. Building gives us full control but adds 3–4 weeks. Auth0 costs \$200/mo at our scale but ships in a day." \
  --hints "#decision #architecture" --mentions "@tech.auth" --profile founder

analyze "Positioning insight: our competitors focus on enterprise search. We win on personal knowledge sovereignty and local-first privacy. That's our moat." \
  --hints "#positioning #strategy" --mentions "@product.positioning" --profile founder

analyze "Q1 priorities: (1) ship pipeline plugins API, (2) onboard 10 design partners, (3) reach 500 MAU on self-hosted. Everything else is noise." \
  --hints "#planning #okr" --mentions "@product.roadmap" --profile founder

analyze "Pricing hypothesis: freemium self-hosted forever, cloud at \$12/mo per seat. Test with design partners in April." \
  --hints "#pricing #hypothesis" --mentions "@product.pricing" --profile founder

analyze "Design partner feedback from @stripe.api team: they want batch import from Notion, RSQL filter by date range, and a Slack bot surface." \
  --hints "#feedback #partners" --mentions "@product.feedback @stripe.api" --profile founder

analyze "Decided: AGPL-3.0 license. Reason: ensures improvements stay open, deters cloud resellers without agreement, aligns with local-first values." \
  --hints "#decision #legal" --mentions "@product.license" --profile founder

analyze "Key metric: time-to-first-insight. User should be able to ingest 10 items and find something useful within 15 minutes of install." \
  --hints "#metrics #ux" --mentions "@product.onboarding" --profile founder

# Engineering / architecture
analyze "ADR draft: use SQLite FTS5 for full-text search on local instances. Rationale: zero-dependency, fast enough under 1M objects, ships with Go's modernc.org/sqlite." \
  --hints "#adr #architecture #sqlite" --mentions "@tech.sqlite @tech.fts5" --profile engineer

analyze "Performance observation: embedding generation is the bottleneck at ~200ms per object with nomic-embed-text on CPU. Consider batching 32 objects per LLM call." \
  --hints "#performance #embeddings" --mentions "@tech.embeddings" --profile engineer

analyze "Bug: pipeline steps run sequentially but could fan out. Refactor StepRunner to use errgroup with bounded concurrency (max 4 workers)." \
  --hints "#bug #pipeline #concurrency" --mentions "@code.pipeline" --profile engineer

analyze "Testing pattern: use go-vcr for all LLM provider tests. Record once with real API key, replay in CI. Cassettes stored in testdata/fixtures/." \
  --hints "#testing #patterns" --mentions "@code.testing" --profile engineer

analyze "Dependency decision: avoid ORMs. Use raw SQL with sqlc for type-safe queries. Rationale: migrations stay readable, query plans are predictable." \
  --hints "#decision #database" --mentions "@tech.sqlc @tech.sqlite" --profile engineer

analyze "Observation: RSQL parser handles 95% of query patterns. The 5% edge case is nested OR groups with entity filters. Add test coverage for: tag:go OR (tag:rust AND mention:@lang.systems)." \
  --hints "#rsql #testing #edge-case" --mentions "@code.query" --profile engineer

analyze "Plugin interface design: steps receive KnowledgeObject, return enriched KnowledgeObject. Side effects go through the event bus, not direct DB writes." \
  --hints "#plugins #design" --mentions "@code.plugins" --profile engineer

analyze "Deployment insight: single binary + SQLite makes self-hosting trivial. Docker image should be under 50MB. Currently at 42MB — good." \
  --hints "#deployment #docker" --mentions "@ops.docker" --profile engineer

# Research / knowledge management
analyze "Paper summary: 'Attention is All You Need' (Vaswani et al., 2017). Introduced the Transformer architecture. Self-attention replaces RNNs for sequence modeling. Now the backbone of all modern LLMs." \
  --hints "#paper #ml #transformers" --mentions "@ml.transformers" --profile researcher

analyze "Research note: local-first software manifesto (Kleppmann et al.). Core thesis: user data should live on user devices. Network is optional, not required. Directly aligned with dPKMS design." \
  --hints "#research #local-first" --mentions "@philosophy.local-first" --profile researcher

analyze "Reading: 'How complex systems fail' (Cook, 1998). Key insight: failures are always the result of multiple contributing factors, never a single root cause." \
  --hints "#resilience #systems" --mentions "@philosophy.systems-thinking" --profile researcher

analyze "Concept: Zettelkasten method. Each note = one atomic idea. Notes link to each other. Emergence from connections, not hierarchy. Contrast with traditional folders." \
  --hints "#pkm #zettelkasten" --mentions "@method.zettelkasten" --profile researcher

analyze "Research question: how do knowledge workers currently discover serendipitous connections in their notes? Most tools lack proactive surfacing. ContextHelp's resurfacing engine addresses this." \
  --hints "#research #ux #resurfacing" --mentions "@product.resurfacing" --profile researcher

# URL captures
analyze "https://go.dev/blog/context" \
  --type url --hints "#go #concurrency" --mentions "@lang.go" --profile engineer

analyze "https://martinfowler.com/articles/eventsourcing.html" \
  --type url --hints "#architecture #event-sourcing" --mentions "@pattern.event-sourcing" --profile engineer

analyze "https://www.sqlite.org/fts5.html" \
  --type url --hints "#sqlite #fts #search" --mentions "@tech.sqlite @tech.fts5" --profile engineer

analyze "https://pkg.go.dev/golang.org/x/sync/errgroup" \
  --type url --hints "#go #concurrency #errgroup" --mentions "@lang.go" --profile engineer

analyze "https://en.wikipedia.org/wiki/CRDT" \
  --type url --hints "#distributed #crdt #local-first" --mentions "@tech.crdt" --profile researcher

# ─────────────────────────────────────────────
# 7. Import: batch synthetic JSONL (50+ items)
# ─────────────────────────────────────────────
section "7. Batch import (synthetic JSONL)"

BATCH_FILE="$(mktemp /tmp/ctxt-seed-XXXXXX.jsonl)"
trap 'rm -f "$BATCH_FILE"' EXIT

cat > "$BATCH_FILE" << 'JSONL'
{"type":"text","content":"Decision: use outbox pattern for reliable job enqueue. Write job row in same transaction as KnowledgeObject insert. Worker polls outbox.","tags":["decision","architecture","reliability"],"mentions":["@pattern.outbox"]}
{"type":"text","content":"Insight: users who set a reminder on an object return to the app 3x more often. Reminders are a retention mechanic, not just a utility.","tags":["insight","retention","reminders"],"mentions":["@product.reminders"]}
{"type":"text","content":"Hypothesis: knowledge workers save 40% more items than they ever revisit. The problem isn't capture — it's rediscovery.","tags":["hypothesis","ux","resurfacing"],"mentions":["@product.resurfacing"]}
{"type":"text","content":"Architecture: ctxt (brain) decides what to do; dPKMS (substrate) ensures it's done safely. Clean separation allows independent evolution.","tags":["architecture","design"],"mentions":["@code.architecture"]}
{"type":"text","content":"Go module tip: use go work for monorepo-style multi-module development. go.work replaces replace directives in individual go.mod files.","tags":["go","tooling","modules"],"mentions":["@lang.go"]}
{"type":"text","content":"User story pattern: 'As a [persona], I want [goal] so that [outcome].' Acceptance criteria must be testable. Every story needs an E2E checklist.","tags":["process","stories","testing"],"mentions":["@method.user-stories"]}
{"type":"text","content":"Recall: RRF (Reciprocal Rank Fusion) combines multiple ranked lists. Score = sum(1/(k+rank_i)). k=60 is empirically robust. Used in ctxt reranking.","tags":["search","reranking","ml"],"mentions":["@tech.rrf"]}
{"type":"text","content":"Observation: semantic search alone has low precision for domain-specific queries. Hybrid search (BM25 + vector) consistently outperforms either alone.","tags":["search","hybrid","embeddings"],"mentions":["@tech.hybrid-search"]}
{"type":"text","content":"Design decision: knowledge objects are immutable after enrichment. Edits create a new version. Provenance is never lost.","tags":["decision","design","provenance"],"mentions":["@code.knowledge-object"]}
{"type":"text","content":"Go tip: use sync.WaitGroup to drain in-flight goroutines before stopping a worker. Prevents data races between goroutine writes and test reads.","tags":["go","concurrency","testing"],"mentions":["@lang.go"]}
{"type":"text","content":"Principle: never mock the database in integration tests. Use real SQLite in-memory DB. Mocked tests pass when prod migrations fail — learned from experience.","tags":["testing","principle","integration"],"mentions":["@code.testing"]}
{"type":"text","content":"Feature: pipeline steps are composable. Each step receives a KnowledgeObject and returns one. Steps can be chained, parallelized, or skipped by capability probe.","tags":["pipeline","design","extensibility"],"mentions":["@code.pipeline"]}
{"type":"text","content":"Product insight: the value of a knowledge graph compounds over time. Early users see little value; power users with 1000+ objects see exponential recall improvement.","tags":["product","insight","value"],"mentions":["@product.knowledge-graph"]}
{"type":"text","content":"Onboarding friction: users don't understand 'focus profile' on first use. Rename to 'context lens' or add an onboarding wizard that auto-suggests based on first 5 captures.","tags":["ux","onboarding","friction"],"mentions":["@product.onboarding"]}
{"type":"text","content":"Infrastructure: SQLite WAL mode enables concurrent reads with one writer. Essential for dpkms serve running alongside ctxt CLI commands.","tags":["sqlite","infrastructure","concurrency"],"mentions":["@tech.sqlite"]}
{"type":"text","content":"Competitive analysis: Obsidian has 1M+ users but no structured entity system. Notion has databases but no semantic search. ContextHelp is the semantic layer on top of both.","tags":["competitive","positioning"],"mentions":["@product.positioning"]}
{"type":"text","content":"Reminder to self: test the pipeline with a 10MB PDF. FlatReader step may OOM on large binary blobs. Add a size check + warning at 5MB.","tags":["bug","pipeline","pdf"],"mentions":["@code.pipeline"]}
{"type":"text","content":"Entity resolution: @stripe.api resolves to canonical entity with stable ID. Any content mentioning 'Stripe' or 'stripe-api' auto-backlinks. This is the DNS-for-concepts model.","tags":["entities","knowledge-graph","design"],"mentions":["@stripe.api"]}
{"type":"text","content":"Metric target: p99 search latency < 200ms on 100k objects with FTS5 + vector reranking. Current baseline: 80ms at 10k objects. Extrapolation looks acceptable.","tags":["performance","metrics","search"],"mentions":["@product.search"]}
{"type":"text","content":"Decision: ship gRPC and REST APIs simultaneously. REST for CLI and web clients; gRPC for agent integrations and high-throughput pipelines.","tags":["decision","api","grpc"],"mentions":["@code.api"]}
{"type":"text","content":"Learning: LMQL enables token-level logit masking for local models. Forces output to conform to schema without retries. Fallback to instructor for API-hosted models.","tags":["ml","lmql","constrained-generation"],"mentions":["@tech.lmql"]}
{"type":"text","content":"Go pattern: use functional options (WithXxx) for optional configuration in constructors. Avoids large config structs that accumulate zero values over time.","tags":["go","patterns","design"],"mentions":["@lang.go"]}
{"type":"text","content":"Research: vector databases don't replace full-text search — they complement it. Keyword recall catches exact matches vectors miss. Always hybrid.","tags":["search","research","embeddings"],"mentions":["@tech.hybrid-search"]}
{"type":"text","content":"Process note: user stories must reference ADRs or document their spec source. Stories without a spec anchor drift over time as implementation changes.","tags":["process","stories","documentation"],"mentions":["@method.user-stories"]}
{"type":"text","content":"Deployment tip: health check endpoint should verify DB connection, not just HTTP 200. Use /healthz that pings SQLite and returns service status JSON.","tags":["ops","deployment","health"],"mentions":["@ops.healthcheck"]}
{"type":"text","content":"Insight: 'ctxt find' is the most-used command in early testing. Users type natural queries and expect Google-like results. Invest heavily in NLQ quality.","tags":["ux","search","insight"],"mentions":["@product.search"]}
{"type":"text","content":"Architecture note: the registry federation layer works like DNS. You subscribe to registries; they resolve @namespace.slug to canonical entities. Caching + TTL handled by dPKMS.","tags":["architecture","registries","federation"],"mentions":["@code.registry"]}
{"type":"text","content":"Go: avoid global state in package-level vars. Prefer dependency injection via constructor args. Makes testing trivial and avoids init() ordering surprises.","tags":["go","testing","patterns"],"mentions":["@lang.go"]}
{"type":"text","content":"UX observation: users want to 'star' objects to pin them. Map to explicit tag 'starred' internally. No special storage needed — tags are first-class.","tags":["ux","feature-request","tags"],"mentions":["@product.tags"]}
{"type":"text","content":"Security: encrypt sensitive fields at rest using per-object keys wrapped by user master key. dPKMS security layer handles key management transparently.","tags":["security","encryption","design"],"mentions":["@code.security"]}
{"type":"text","content":"Resurfacing algorithm: score = 0.4*entity_overlap + 0.3*tag_overlap + 0.3*recency_decay. Weights tuned empirically. Graph proximity and interaction scores deferred to Phase 3.","tags":["algorithm","resurfacing","scoring"],"mentions":["@code.resurfacing"]}
{"type":"text","content":"Content type insight: URL captures are the highest-signal objects. Users bookmark URLs with intent. Prioritize URL pipeline quality over plain-text quality.","tags":["insight","pipeline","urls"],"mentions":["@code.pipeline"]}
{"type":"text","content":"Testing pyramid: 70% unit, 20% integration, 10% smoke/e2e. Integration tests use real SQLite. Smoke tests run against a live dpkms serve instance.","tags":["testing","strategy","pyramid"],"mentions":["@code.testing"]}
{"type":"text","content":"Go module layout: cmd/ for binaries, internal/ for private packages, pkg/ for public API (none yet), docs/ for documentation, scripts/ for tooling.","tags":["go","project-structure","layout"],"mentions":["@lang.go"]}
{"type":"text","content":"Feature: ctxt make brief generates executive summaries with inline [ref:ID] citations. Users can trace every claim back to the source object.","tags":["feature","composition","citations"],"mentions":["@product.make"]}
{"type":"text","content":"Observation: knowledge workers use 3-5 different tools daily (Notion, Obsidian, email, Slack, browser). ContextHelp must ingest from all of them to be the unified layer.","tags":["observation","ux","integrations"],"mentions":["@product.integrations"]}
{"type":"text","content":"Principle: local-first means the app works without internet. Registry sync, LLM calls, and embedding generation are optional network operations. Core read/write always local.","tags":["principle","local-first","offline"],"mentions":["@philosophy.local-first"]}
{"type":"text","content":"Go: use context.Context for cancellation, deadlines, and request-scoped values. Never store context in a struct. Pass it as the first argument to every function that does I/O.","tags":["go","context","patterns"],"mentions":["@lang.go"]}
{"type":"text","content":"Schema evolution: knowledge objects versioned via migration number in schema_version column. Migrations are append-only. Never drop columns — mark deprecated and ignore in code.","tags":["database","migrations","design"],"mentions":["@code.storage"]}
{"type":"text","content":"Founder note: the hardest part of building a developer tool is the onboarding funnel. Developers evaluate tools in 10 minutes. If they don't see value, they never come back.","tags":["founder","onboarding","strategy"],"mentions":["@product.onboarding"]}
{"type":"text","content":"Research: spaced repetition is more effective than passive review for knowledge retention. Resurfacing engine should implement spaced repetition scheduling in future.","tags":["research","spaced-repetition","resurfacing"],"mentions":["@product.resurfacing"]}
{"type":"text","content":"CLI design principle: every command should have --dry-run, --output json, and --verbose flags. Composability with jq is a first-class requirement.","tags":["cli","design","ux"],"mentions":["@product.cli"]}
{"type":"text","content":"Pipeline decision: FileReader step should early-return if RawContent is already set. Prevents redundant reads when content was provided inline at enqueue time.","tags":["pipeline","decision","optimization"],"mentions":["@code.pipeline"]}
{"type":"text","content":"Entity backlinks enable power users to answer: 'show me everything related to @stripe.api'. This is the knowledge graph query that makes ctxt different from plain search.","tags":["entities","knowledge-graph","feature"],"mentions":["@stripe.api"]}
{"type":"text","content":"Infrastructure: WAL checkpoint should run nightly via dpkms housekeeping. Prevents WAL file from growing unbounded. Default checkpoint threshold: 1000 pages.","tags":["sqlite","ops","housekeeping"],"mentions":["@tech.sqlite"]}
{"type":"text","content":"UX: natural language time parsing for reminders. 'in 2h', 'tomorrow 9am', 'monday 14:30' all work. Users should never need to type ISO timestamps manually.","tags":["ux","reminders","nlp"],"mentions":["@product.reminders"]}
{"type":"text","content":"Go: prefer table-driven tests. Each test case is a struct with input, expected output, and name. Makes it trivial to add new cases without changing test logic.","tags":["go","testing","patterns"],"mentions":["@lang.go"]}
{"type":"text","content":"Observation: the most useful knowledge objects are decisions and open questions. Summaries are noise; decisions and questions drive action.","tags":["observation","knowledge","quality"],"mentions":["@product.knowledge-graph"]}
JSONL

log "importing batch JSONL ($(wc -l < "$BATCH_FILE") items)"
$CTXT import batch --file "$BATCH_FILE" --server "$SERVER"

# ─────────────────────────────────────────────
# 8. Wait briefly for worker to pick up jobs
# ─────────────────────────────────────────────
section "8. Waiting for worker"
log "sleeping 5s for dpkms serve to start processing..."
sleep 5

# ─────────────────────────────────────────────
# 9. Set reminders on a few seed objects
# ─────────────────────────────────────────────
section "9. Set reminders"

# Get IDs of recently ingested objects to remind
REMIND_IDS=$($CTXT list --output json 2>/dev/null | \
  grep -o '"id":"[^"]*"' | head -5 | grep -o '"[^"]*"$' | tr -d '"' || true)

i=1
for id in $REMIND_IDS; do
  case $i in
    1) time_expr="tomorrow 9am" ;;
    2) time_expr="in 2h" ;;
    3) time_expr="monday 14:00" ;;
    4) time_expr="in 30m" ;;
    5) time_expr="2026-04-07 10:00" ;;
  esac
  log "setting reminder on $id: $time_expr"
  $CTXT remind "$id" "$time_expr" || log "  (skipped)"
  i=$((i+1))
done

# ─────────────────────────────────────────────
# 10. Resurface refresh
# ─────────────────────────────────────────────
section "10. Resurface refresh"

log "running resurface refresh for founder profile"
$CTXT resurface refresh --profile founder || log "  (no objects scored yet — run again after worker processes jobs)"

# ─────────────────────────────────────────────
# 11. Verify: list, find, reminders, resurface
# ─────────────────────────────────────────────
section "11. Verification"

log "listing recent objects (last 10)"
$CTXT list --limit 10 || true

log "searching: 'local-first architecture'"
$CTXT find "local-first architecture" --limit 5 || true

log "searching: 'sqlite performance'"
$CTXT find "sqlite performance" --limit 5 || true

log "listing pending reminders"
$CTXT reminders || true

log "showing resurface candidates"
$CTXT resurface --limit 5 --profile founder || true

log "listing entities"
$CTXT entity list || true

log "listing feeds"
$CTXT feed list || true

# ─────────────────────────────────────────────
# 12. Make compositions (requires enriched objects)
# ─────────────────────────────────────────────
section "12. Compositions (may be empty if worker hasn't processed yet)"

log "making a brief on 'architecture' tag"
$CTXT make brief --tag architecture --no-citations || log "  (no objects ready yet)"

log "making a plan on 'decision' tag"
$CTXT make plan --tag decision --no-citations || log "  (no objects ready yet)"

# ─────────────────────────────────────────────
# Done
# ─────────────────────────────────────────────
section "Seed complete"
echo "Run 'ctxt list' and 'ctxt find <query>' to explore your knowledge base."
echo "Run 'ctxt jobs' to monitor ingestion progress."
echo ""
echo "TODO: add Twitter and Google Drive imports when credentials are available:"
echo "  ctxt import twitter --file /path/to/tweets.js --username jadb"
echo "  ctxt import gdrive --access-token \$GDRIVE_ACCESS_TOKEN --max-items 200"

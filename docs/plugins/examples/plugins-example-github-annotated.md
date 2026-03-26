# GitHub Plugin — Annotated Walkthrough

> Tutorial: build a GitHub plugin from scratch. Every design decision explained
> inline. Use as a reference when creating any external plugin.
>
> Reference files:
> - [plugins-api.md](../plugins-api.md) — stable Plugin API contract
> - [plugins-github-pr-review-watcher.md](../plugins-github-pr-review-watcher.md) — full
>   spec this tutorial is based on
> - [plugins-sample-price-monitor.md](plugins-sample-price-monitor.md) — simpler example
>   for contrast

---

## What we are building

A plugin that monitors GitHub pull-request reviews in configured repositories,
extracts team conventions and actionable insights, and surfaces them in `ctxt`.

Simplified scope for this tutorial (the full watcher spec adds pattern-frequency
tracking, notification integration, and embedding-based dedup — omitted here to
keep the walkthrough focused).

---

## Table of contents

1. [Directory layout](#1-directory-layout)
2. [Plugin manifest](#2-plugin-manifest)
3. [Configuration model](#3-configuration-model)
4. [Plugin struct and Init](#4-plugin-struct-and-init)
5. [GitHub authentication](#5-github-authentication)
6. [Pipeline step: fetch reviews](#6-pipeline-step-fetch-reviews)
7. [Pipeline step: analyze review](#7-pipeline-step-analyze-review)
8. [PostIngest hook](#8-postingest-hook)
9. [Scheduled polling](#9-scheduled-polling)
10. [Webhook vs polling decision](#10-webhook-vs-polling-decision)
11. [Error handling and retries](#11-error-handling-and-retries)
12. [State management](#12-state-management)
13. [Testing locally](#13-testing-locally)
14. [Wiring it into ctxt](#14-wiring-it-into-ctxt)

---

## 1. Directory layout

```
~/.contexthelp/plugins/github-pr-watcher/
  plugin.json        # manifest — declares name, hooks, pipelines, permissions
  state.json         # plugin-owned mutable state (sync cursors, pattern counts)
  cache/             # raw API responses; safe to delete
  logs/              # plugin log output

# Source (your repo)
github-pr-watcher/
  plugin.go          # Plugin struct + Init + Close
  config.go          # typed config structs
  auth.go            # GitHub auth helpers
  fetch.go           # FetchReviews pipeline step
  analyze.go         # AnalyzeReview pipeline step
  state.go           # state.json read/write helpers
  webhook.go         # optional webhook server
  plugin_test.go
```

Rule: every piece of data the plugin creates lives under
`~/.contexthelp/plugins/github-pr-watcher/`. Plugins MUST NOT read or write
outside this sandbox unless the user explicitly configures an override path.

---

## 2. Plugin manifest

`plugin.json` is the contract between the plugin and the core runtime.

```json
{
  "name": "github-pr-watcher",
  "version": "1.0.0",
  "description": "Monitors GitHub PR reviews; extracts team conventions and insights",
  "entrypoint": "code.so",
  "hooks": [
    "post_ingest",
    "scheduled"
  ],
  "pipelines": [
    "github.pr_review.fetch",
    "github.pr_review.analyze"
  ],
  "object_types": [
    "pr-review-insight"
  ],
  "permissions": {
    "network": true,
    "filesystem": true
  },
  "requires_plugin_api": ">=1.0,<2.0"
}
```

Annotations:

- `hooks` lists only what you implement. Declaring an unused hook is harmless
  but misleading — keep it minimal.
- `pipelines` are just names; the runtime looks them up via `PipelineSteps()`.
- `object_types` tells the runtime which custom types this plugin can create.
  Core will reject objects of undeclared types at ingest time.
- `permissions.network: true` is required for any outbound HTTP. The runtime
  blocks syscalls when this is false, so be explicit.
- `requires_plugin_api` uses semver ranges. Pin the minor you tested against;
  allow all patches in that minor.

---

## 3. Configuration model

Users configure the plugin under `plugins.github-pr-watcher` in `ctxt.yaml`.

```yaml
plugins:
  github-pr-watcher:
    enabled: true
    mode: polling          # "polling" | "webhook"
    polling_interval_seconds: 900

    repositories:
      - owner: myorg
        repo: backend-api
        branch_filter: [main, develop]

    analysis:
      min_confidence: 0.7
      categories: [security, performance, architecture, code_style, testing]

    surfacing:
      auto_surface_threshold: 0.8
      max_insights_per_day: 10

    privacy:
      store_author_names: true
      anonymize_after_days: 90

    github:
      token: ${GITHUB_TOKEN}            # env var reference — never hard-code
      webhook_secret: ${GH_WEBHOOK_SECRET}
```

Map this to Go structs in `config.go`:

```go
// Config is the top-level config block for this plugin.
// plugin.ConfigTyped(&cfg) populates it from ctxt.yaml at Init time.
type Config struct {
    Enabled                bool           `json:"enabled"`
    Mode                   string         `json:"mode"`           // "polling"|"webhook"
    PollingIntervalSeconds int            `json:"polling_interval_seconds"`
    Repositories           []RepoConfig   `json:"repositories"`
    Analysis               AnalysisConfig `json:"analysis"`
    Surfacing              SurfaceConfig  `json:"surfacing"`
    Privacy                PrivacyConfig  `json:"privacy"`
    GitHub                 GitHubCreds    `json:"github"`
}

type RepoConfig struct {
    Owner         string   `json:"owner"`
    Repo          string   `json:"repo"`
    BranchFilter  []string `json:"branch_filter"`
}

type GitHubCreds struct {
    Token         string `json:"token"`          // resolved from env by ctxt
    WebhookSecret string `json:"webhook_secret"` // resolved from env by ctxt
}
```

Design note: `token` in YAML references `${GITHUB_TOKEN}`. The ctxt config
resolver expands env refs before passing the map to `plugin.Config()`, so your
struct receives the plaintext value. Never log it.

---

## 4. Plugin struct and Init

```go
package ghprwatcher

import (
    "context"
    "encoding/json"
    "fmt"
    "os"
    "path/filepath"

    "github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
)

// Plugin implements pluginapi.Plugin (and optionally PostIngestHook).
// Keep the struct small — heavyweight state belongs in state.json.
type Plugin struct {
    cfg     Config
    stateDir string              // ~/.contexthelp/plugins/github-pr-watcher
    deps    pluginapi.Deps
}

// Name must match the "name" field in plugin.json and the config key.
// The runtime uses this to look up the config block and avoid name collisions.
func (p *Plugin) Name() string    { return "github-pr-watcher" }
func (p *Plugin) Version() string { return "1.0.0" }

// Init is called once at startup. Validate config here; fail fast.
// deps.Bus is the event bus for publishing/subscribing to CloudEvents.
// deps.Store gives access to the alias store (only — not raw DB).
func (p *Plugin) Init(ctx context.Context, cfg map[string]interface{}, deps pluginapi.Deps) error {
    // 1. Deserialise config from the raw map.
    raw, err := json.Marshal(cfg)
    if err != nil {
        return fmt.Errorf("github-pr-watcher: marshal config: %w", err)
    }
    if err := json.Unmarshal(raw, &p.cfg); err != nil {
        return fmt.Errorf("github-pr-watcher: unmarshal config: %w", err)
    }

    // 2. Validate required fields early. Surface clear errors, not nil-dereferences.
    if p.cfg.GitHub.Token == "" {
        return fmt.Errorf("github-pr-watcher: github.token is required (set GITHUB_TOKEN)")
    }
    if len(p.cfg.Repositories) == 0 {
        return fmt.Errorf("github-pr-watcher: at least one repository must be configured")
    }

    // 3. Set defaults for optional fields.
    if p.cfg.PollingIntervalSeconds == 0 {
        p.cfg.PollingIntervalSeconds = 900 // 15 min
    }
    if p.cfg.Surfacing.AutoSurfaceThreshold == 0 {
        p.cfg.Surfacing.AutoSurfaceThreshold = 0.8
    }

    // 4. Locate plugin sandbox directory.
    home, err := os.UserHomeDir()
    if err != nil {
        return fmt.Errorf("github-pr-watcher: user home: %w", err)
    }
    p.stateDir = filepath.Join(home, ".contexthelp", "plugins", "github-pr-watcher")
    if err := os.MkdirAll(filepath.Join(p.stateDir, "cache"), 0o700); err != nil {
        return fmt.Errorf("github-pr-watcher: create dirs: %w", err)
    }

    p.deps = deps
    return nil
}

// Close is called on graceful shutdown. Release any open connections.
func (p *Plugin) Close(ctx context.Context) error {
    // Nothing persistent to release in this minimal example.
    return nil
}
```

Key takeaways from Init:

- Marshal + Unmarshal is the idiomatic way to go from `map[string]interface{}`
  to a typed struct. Avoids reflection, keeps compile-time safety.
- Validate eagerly. A bad token fails at startup, not 15 minutes later at the
  first scheduled run.
- Default values in Init keep YAML simple for users while keeping code safe.

---

## 5. GitHub authentication

`auth.go` — thin wrapper so the rest of the plugin does not touch tokens directly.

```go
package ghprwatcher

import (
    "context"
    "fmt"
    "net/http"
    "time"
)

// newHTTPClient returns an authenticated http.Client for the GitHub REST API.
// Uses the token from config; caller must NOT log the token.
func (p *Plugin) newHTTPClient() *http.Client {
    transport := &tokenTransport{token: p.cfg.GitHub.Token}
    return &http.Client{
        Timeout:   30 * time.Second, // hard cap per request; prevents hung goroutines
        Transport: transport,
    }
}

// tokenTransport injects Authorization: Bearer <token> on every request.
type tokenTransport struct {
    token string
    base  http.RoundTripper
}

func (t *tokenTransport) RoundTrip(req *http.Request) (*http.Response, error) {
    // Clone before mutating — http.Request is not goroutine-safe.
    r := req.Clone(req.Context())
    r.Header.Set("Authorization", "Bearer "+t.token)
    r.Header.Set("Accept", "application/vnd.github+json")
    r.Header.Set("X-GitHub-Api-Version", "2022-11-28") // pin API version
    base := t.base
    if base == nil {
        base = http.DefaultTransport
    }
    return base.RoundTrip(r)
}

// validateToken checks the token is usable before any polling starts.
// Avoids silent 401s during polling runs.
func (p *Plugin) validateToken(ctx context.Context) error {
    client := p.newHTTPClient()
    req, err := http.NewRequestWithContext(ctx, http.MethodGet,
        "https://api.github.com/user", nil)
    if err != nil {
        return err
    }
    resp, err := client.Do(req)
    if err != nil {
        return fmt.Errorf("github-pr-watcher: token check: %w", err)
    }
    defer resp.Body.Close()
    if resp.StatusCode == http.StatusUnauthorized {
        return fmt.Errorf("github-pr-watcher: token invalid or expired (HTTP 401)")
    }
    return nil
}
```

Why a custom transport instead of just setting the header in every request:

- Centralises auth logic; changing from PAT to GitHub App requires editing one place.
- Prevents accidental token leaks (the token never appears outside this file).
- Easy to swap for test: inject a mock base transport.

Token rotation: if your org uses short-lived tokens, refresh in `RoundTrip` by
checking expiry before injecting the header and calling a refresh endpoint.
Store the refreshed token in state.json (encrypted).

---

## 6. Pipeline step: fetch reviews

Pipeline steps implement `pluginapi.PipelineStep`. Each step receives a
`*KnowledgeObject` and returns an enriched copy.

```go
package ghprwatcher

import (
    "context"
    "encoding/json"
    "fmt"
    "io"
    "net/http"
    "time"

    "github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
)

// FetchReviews is pipeline step "github.pr_review.fetch".
// Input:  a KnowledgeObject of type "github-repo-config" carrying owner/repo.
// Output: same object with Metadata["reviews"] populated.
type FetchReviews struct {
    plugin *Plugin
}

func (s *FetchReviews) Name() string { return "github.pr_review.fetch" }

func (s *FetchReviews) Contract() pluginapi.StepContract {
    return pluginapi.StepContract{
        Requires:     []string{"metadata"},  // reads Metadata["owner"], ["repo"]
        Produces:     []string{"metadata"},  // writes Metadata["reviews"]
        Capabilities: []string{"network"},
    }
}

// Run fetches PR reviews from the GitHub REST API since last sync cursor.
func (s *FetchReviews) Run(ctx context.Context, draft *pluginapi.KnowledgeObject) (
    *pluginapi.KnowledgeObject, error) {

    owner, _ := draft.Metadata["owner"].(string)
    repo, _ := draft.Metadata["repo"].(string)
    if owner == "" || repo == "" {
        return nil, fmt.Errorf("fetch-reviews: owner/repo missing in metadata")
    }

    // Load the last sync cursor so we only fetch new reviews.
    // cursor is a Unix timestamp; 0 means first run (fetch last 7 days).
    cursor := s.plugin.loadCursor(owner, repo)
    since := time.Unix(cursor, 0)
    if cursor == 0 {
        since = time.Now().AddDate(0, 0, -7)
    }

    reviews, err := s.fetchSince(ctx, owner, repo, since)
    if err != nil {
        return nil, fmt.Errorf("fetch-reviews %s/%s: %w", owner, repo, err)
    }

    // Persist cursor before returning so a downstream failure doesn't re-fetch.
    // Idempotency: storing the cursor early means we may miss a review if
    // analysis crashes mid-way. Acceptable trade-off vs infinite re-fetch loops.
    s.plugin.storeCursor(owner, repo, time.Now().Unix())

    if draft.Metadata == nil {
        draft.Metadata = make(map[string]any)
    }
    draft.Metadata["reviews"] = reviews
    return draft, nil
}

// ghReview mirrors only the fields we need from the GitHub API response.
type ghReview struct {
    ID          int64     `json:"id"`
    User        ghUser    `json:"user"`
    Body        string    `json:"body"`
    State       string    `json:"state"` // APPROVED, CHANGES_REQUESTED, COMMENTED
    SubmittedAt time.Time `json:"submitted_at"`
    HTMLURL     string    `json:"html_url"`
    PRURL       string    `json:"pull_request_url"`
}

type ghUser struct {
    Login string `json:"login"`
}

// fetchSince calls GET /repos/{owner}/{repo}/pulls/*/reviews for all open PRs
// updated after `since`. GitHub does not expose a single reviews-since endpoint,
// so we walk open PRs first (paginated), then fetch reviews per PR.
func (s *FetchReviews) fetchSince(ctx context.Context, owner, repo string,
    since time.Time) ([]ghReview, error) {

    client := s.plugin.newHTTPClient()

    // Step 1: list recently-updated PRs.
    prs, err := s.listPRs(ctx, client, owner, repo, since)
    if err != nil {
        return nil, err
    }

    // Step 2: for each PR, fetch reviews.
    var all []ghReview
    for _, pr := range prs {
        reviews, err := s.listReviews(ctx, client, owner, repo, pr, since)
        if err != nil {
            // Non-fatal: log and skip this PR; don't abort the whole batch.
            s.plugin.log("warn", fmt.Sprintf(
                "fetch-reviews: skipping PR #%d: %v", pr, err))
            continue
        }
        all = append(all, reviews...)
    }
    return all, nil
}

// listPRs returns PR numbers updated after `since`.
func (s *FetchReviews) listPRs(ctx context.Context, client *http.Client,
    owner, repo string, since time.Time) ([]int, error) {

    url := fmt.Sprintf(
        "https://api.github.com/repos/%s/%s/pulls?state=all&sort=updated&direction=desc&per_page=100",
        owner, repo)

    // Paginated GET. Use Link header for next page.
    // This loop terminates when:
    //   (a) no more pages, or
    //   (b) the oldest PR on the page was updated before `since`
    var prNums []int
    for url != "" {
        req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
        resp, err := client.Do(req)
        if err != nil {
            return nil, err
        }
        if resp.StatusCode != http.StatusOK {
            resp.Body.Close()
            return nil, fmt.Errorf("list-prs: HTTP %d", resp.StatusCode)
        }

        var page []struct {
            Number    int       `json:"number"`
            UpdatedAt time.Time `json:"updated_at"`
        }
        if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
            resp.Body.Close()
            return nil, err
        }
        resp.Body.Close()

        done := false
        for _, pr := range page {
            if pr.UpdatedAt.Before(since) {
                done = true
                break
            }
            prNums = append(prNums, pr.Number)
        }
        if done {
            break
        }
        url = parseLinkNext(resp.Header.Get("Link"))
    }
    return prNums, nil
}

// listReviews fetches reviews for a single PR, filtering by submitted_at > since.
func (s *FetchReviews) listReviews(ctx context.Context, client *http.Client,
    owner, repo string, prNum int, since time.Time) ([]ghReview, error) {

    url := fmt.Sprintf(
        "https://api.github.com/repos/%s/%s/pulls/%d/reviews?per_page=100",
        owner, repo, prNum)

    req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
    resp, err := client.Do(req)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()
    if resp.StatusCode == http.StatusNotFound {
        // PR may have been deleted; skip silently.
        return nil, nil
    }
    if resp.StatusCode != http.StatusOK {
        return nil, fmt.Errorf("list-reviews PR#%d: HTTP %d", prNum, resp.StatusCode)
    }

    var reviews []ghReview
    if err := json.NewDecoder(resp.Body).Decode(&reviews); err != nil {
        return nil, err
    }

    // Filter to reviews submitted after cursor.
    var filtered []ghReview
    for _, r := range reviews {
        if r.SubmittedAt.After(since) {
            filtered = append(filtered, r)
        }
    }
    return filtered, nil
}

// parseLinkNext parses the GitHub pagination Link header to extract the next URL.
// Format: <https://api.github.com/...?page=2>; rel="next", <...>; rel="last"
func parseLinkNext(header string) string {
    // Pseudocode — use regexp or stdlib link parser in production.
    // Search for rel="next" segment and extract the URL between < and >.
    // Return "" when no next page.
    return "" // stub
}

// io.Discard drain — used after resp.Body.Close() for clarity
var _ io.Writer = io.Discard
```

Design decisions annotated above:

- Cursor per repo, not global. Repos sync independently; a failure in one
  shouldn't block others.
- Non-fatal per-PR errors. A 404 on a deleted PR should not abort the whole sync.
- Cursor stored before analysis, not after. Avoids infinite re-fetch if
  downstream analysis panics. Insight loss on a crash is preferable to
  a stuck polling loop.

---

## 7. Pipeline step: analyze review

```go
package ghprwatcher

import (
    "context"
    "fmt"
    "strings"

    "github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
)

// AnalyzeReview is pipeline step "github.pr_review.analyze".
// Converts raw review structs into pr-review-insight KnowledgeObjects.
type AnalyzeReview struct {
    plugin *Plugin
}

func (s *AnalyzeReview) Name() string { return "github.pr_review.analyze" }

func (s *AnalyzeReview) Contract() pluginapi.StepContract {
    return pluginapi.StepContract{
        Requires: []string{"metadata"},   // reads Metadata["reviews"]
        Produces: []string{               // writes all core fields
            "type", "title", "text_content", "tags", "mentions", "plugins",
        },
    }
}

// Run processes one review per invocation. The pipeline runner calls this
// once per object; if the object carries multiple reviews (set by FetchReviews),
// we fan-out: emit one knowledge object per review.
//
// Fan-out pattern: return a "batch" virtual object whose Metadata["fanout"]
// key triggers core to call PostIngest once per child. Alternatively, the
// scheduled hook (section 9) creates one ingest job per review directly.
// Here we use direct creation via deps.Bus.
func (s *AnalyzeReview) Run(ctx context.Context, draft *pluginapi.KnowledgeObject) (
    *pluginapi.KnowledgeObject, error) {

    reviews, ok := draft.Metadata["reviews"].([]ghReview)
    if !ok || len(reviews) == 0 {
        // Nothing to do; return the draft unchanged.
        return draft, nil
    }

    owner, _ := draft.Metadata["owner"].(string)
    repo, _ := draft.Metadata["repo"].(string)

    for _, review := range reviews {
        obj := s.reviewToObject(owner, repo, review)
        // Publish an ingestion event so core stores the insight.
        // This is the cleanest way to produce multiple outputs from one step.
        if err := s.plugin.publishIngestEvent(ctx, obj); err != nil {
            s.plugin.log("warn", fmt.Sprintf(
                "analyze-review: publish insight %d: %v", review.ID, err))
        }
    }
    return draft, nil
}

// reviewToObject converts one GitHub review into a pr-review-insight object.
func (s *AnalyzeReview) reviewToObject(owner, repo string, r ghReview) *pluginapi.KnowledgeObject {
    // Category classification is intentionally simple here.
    // In production: call an LLM step or rules-based classifier.
    category := classifyCategory(r.Body)
    score := scoreQuality(r)

    obj := &pluginapi.KnowledgeObject{
        // Type must match the object_types declared in plugin.json.
        Type: "pr-review-insight",

        // Title: first sentence of the review body, max 120 chars.
        Title: truncate(firstSentence(r.Body), 120),

        TextContent: r.Body,

        Source: r.HTMLURL,

        Tags: []pluginapi.Tag{
            {Label: category, Source: "github-pr-watcher"},
        },

        // Mentions link this insight to entities in the knowledge graph.
        // @github.<login> — reviewer entity
        // @codebase.<owner>/<repo> — repository entity
        Mentions: []interface{}{
            fmt.Sprintf("github.%s", r.User.Login),
            fmt.Sprintf("codebase.%s/%s", owner, repo),
        },

        // Plugin-specific metadata lives under Plugins[<name>].
        // Core never reads this field; it is owned entirely by this plugin.
        Plugins: map[string]any{
            "github-pr-watcher": map[string]any{
                "pr_url":       r.PRURL,
                "reviewer":     r.User.Login,
                "review_state": r.State,
                "category":     category,
                "quality_score": score,
                "extracted_at": r.SubmittedAt.Unix(),
            },
        },
    }
    return obj
}

// classifyCategory applies keyword rules to assign one of the declared categories.
// Replace with an LLM-backed step for higher accuracy.
func classifyCategory(body string) string {
    lower := strings.ToLower(body)
    switch {
    case strings.Contains(lower, "sql injection") ||
        strings.Contains(lower, "xss") ||
        strings.Contains(lower, "csrf") ||
        strings.Contains(lower, "auth"):
        return "security"
    case strings.Contains(lower, "n+1") ||
        strings.Contains(lower, "index") ||
        strings.Contains(lower, "cache") ||
        strings.Contains(lower, "latency"):
        return "performance"
    case strings.Contains(lower, "async/await") ||
        strings.Contains(lower, "naming") ||
        strings.Contains(lower, "indent"):
        return "code_style"
    case strings.Contains(lower, "test") ||
        strings.Contains(lower, "coverage") ||
        strings.Contains(lower, "mock"):
        return "testing"
    default:
        return "general"
    }
}

// scoreQuality returns a float in [0,1].
// Signals: body length, code blocks, presence of alternatives.
func scoreQuality(r ghReview) float64 {
    score := 0.0
    // Longer reviews are more likely to be actionable.
    if len(r.Body) > 100 {
        score += 0.3
    }
    // Reviews with code examples are highly actionable.
    if strings.Contains(r.Body, "```") {
        score += 0.3
    }
    // Reviewer suggests an alternative, not just a complaint.
    if strings.Contains(strings.ToLower(r.Body), "instead") ||
        strings.Contains(strings.ToLower(r.Body), "prefer") ||
        strings.Contains(strings.ToLower(r.Body), "use ") {
        score += 0.2
    }
    // CHANGES_REQUESTED reviews are typically substantive.
    if r.State == "CHANGES_REQUESTED" {
        score += 0.2
    }
    return score
}

func firstSentence(s string) string {
    if idx := strings.IndexByte(s, '.'); idx >= 0 {
        return s[:idx+1]
    }
    return s
}

func truncate(s string, n int) string {
    if len(s) <= n {
        return s
    }
    return s[:n-3] + "..."
}
```

---

## 8. PostIngest hook

`PostIngest` is called by the runtime after any knowledge object is persisted.
Implement `pluginapi.PostIngestHook` (not just `Plugin`) to receive these calls.

```go
package ghprwatcher

import (
    "context"

    "github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
)

// PostIngest fires after any object is stored. We only care about our own type.
// Keep this fast — it runs synchronously in the ingest path.
func (p *Plugin) PostIngest(ctx context.Context, obj *pluginapi.KnowledgeObject) error {
    if obj.Type != "pr-review-insight" {
        // Ignore everything that is not our type.
        // Other plugins also receive PostIngest; filter by type is mandatory.
        return nil
    }

    meta, _ := obj.Plugins["github-pr-watcher"].(map[string]any)
    if meta == nil {
        return nil
    }

    score, _ := meta["quality_score"].(float64)
    if score >= p.cfg.Surfacing.AutoSurfaceThreshold {
        // High-quality insight: update state so on_cli_start can surface it.
        // Do NOT call notifications API here in the minimal example.
        // In the full watcher spec, this is where you call notifications.Create().
        return p.markPendingSurface(obj.ID)
    }
    return nil
}

// markPendingSurface appends an insight ID to the "pending_surface" list in
// state.json. on_cli_start reads this list and shows a nudge to the user.
func (p *Plugin) markPendingSurface(id string) error {
    state, err := p.loadState()
    if err != nil {
        return err
    }
    pending, _ := state["pending_surface"].([]any)
    pending = append(pending, id)
    state["pending_surface"] = pending
    return p.saveState(state)
}
```

Rules for PostIngest:

- Always filter by type. The hook fires for every object in the system.
- Keep it async-friendly. Use `plugin.EnqueueJob` for any heavy work rather
  than blocking the ingest path.
- Never call the storage layer directly. Manipulate only `state.json` and the
  public APIs.

---

## 9. Scheduled polling

The `scheduled` hook triggers on the interval registered by the plugin.

```go
package ghprwatcher

import (
    "context"
    "fmt"

    "github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
    "hop.top/uri"
)

// PipelineSteps returns the steps registered by this plugin.
// Core calls this once at startup and merges the result into the step registry.
func (p *Plugin) PipelineSteps() []pluginapi.PipelineStep {
    return []pluginapi.PipelineStep{
        &FetchReviews{plugin: p},
        &AnalyzeReview{plugin: p},
    }
}

// scheduledSync is called by the runtime every PollingIntervalSeconds.
// It creates one synthetic ingest object per configured repository, which
// triggers the fetch → analyze pipeline chain.
func (p *Plugin) scheduledSync(ctx context.Context) error {
    for _, repo := range p.cfg.Repositories {
        obj := &pluginapi.KnowledgeObject{
            Type:   "github-repo-config", // internal type; not stored
            Source: fmt.Sprintf("https://github.com/%s/%s", repo.Owner, repo.Repo),
            Metadata: map[string]any{
                "owner": repo.Owner,
                "repo":  repo.Repo,
            },
            Pipeline: "github.pr_review.fetch", // request specific pipeline
        }

        // publishIngestEvent puts the object into the ingest queue.
        // Core picks it up, runs the declared pipeline, calls PostIngest after.
        if err := p.publishIngestEvent(ctx, obj); err != nil {
            // Log and continue — one failing repo should not block others.
            p.log("error", fmt.Sprintf("scheduled-sync %s/%s: enqueue: %v",
                repo.Owner, repo.Repo, err))
        }
    }
    return nil
}

// publishIngestEvent emits a CloudEvent that core's ingest subscriber picks up.
func (p *Plugin) publishIngestEvent(ctx context.Context, obj *pluginapi.KnowledgeObject) error {
    import "encoding/json"
    import "time"
    import "github.com/google/uuid"

    data, err := json.Marshal(obj)
    if err != nil {
        return err
    }
    return p.deps.Bus.Publish(ctx, pluginapi.Event{
        ID:              uuid.NewString(),
        Source:          "github-pr-watcher",
        SpecVersion:     "1.0",
        Type:            "ctxt.ingest.request",
        DataContentType: "application/json",
        Time:            time.Now(),
        Data:            data,
    })
}

// mentionURI formats an @-mention URI for the knowledge graph.
// Follows the dPKMS mention syntax: @<namespace>.<identifier>
func mentionURI(namespace, id string) uri.URI {
    return uri.URI(fmt.Sprintf("%s.%s", namespace, id))
}
```

---

## 10. Webhook vs polling decision

Both modes have trade-offs. Choosing the wrong one causes either missed events
(polling too slow) or infrastructure burden (webhook requires public endpoint).

```
                  Polling                  Webhook
─────────────────────────────────────────────────────────────────
Setup             Zero extra infra         Need public HTTPS endpoint
Latency           Up to interval delay     Near real-time (<5 s)
Reliability       Pull model; self-heals   Push model; miss events on downtime
Auth needed       Token only               Token + webhook secret
Rate limits       Can exhaust REST quota   Uses event API (much lower quota)
Good for          Laptops, local dev,      CI servers, long-lived processes,
                  no public IP             team-shared instances
─────────────────────────────────────────────────────────────────
```

Webhook implementation sketch:

```go
// StartWebhookServer listens for GitHub webhook events and converts them
// to ingest objects. Run this in a goroutine from Init.
func (p *Plugin) StartWebhookServer(addr string) error {
    mux := http.NewServeMux()
    mux.HandleFunc("/github/webhook", p.handleWebhook)
    return http.ListenAndServe(addr, mux) // TLS in production
}

func (p *Plugin) handleWebhook(w http.ResponseWriter, r *http.Request) {
    // 1. Validate HMAC-SHA256 signature to prevent spoofing.
    //    GitHub sends X-Hub-Signature-256: sha256=<hex>
    sig := r.Header.Get("X-Hub-Signature-256")
    if !validateHMAC(sig, p.cfg.GitHub.WebhookSecret, r.Body) {
        http.Error(w, "forbidden", http.StatusForbidden)
        return
    }

    // 2. Check event type — only handle pull_request_review.
    event := r.Header.Get("X-GitHub-Event")
    if event != "pull_request_review" {
        w.WriteHeader(http.StatusNoContent)
        return
    }

    // 3. Decode payload and emit ingest event.
    var payload struct {
        Action string   `json:"action"` // "submitted"
        Review ghReview `json:"review"`
        Repo   struct {
            Owner struct{ Login string } `json:"owner"`
            Name  string                `json:"name"`
        } `json:"repository"`
    }
    if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
        http.Error(w, "bad request", http.StatusBadRequest)
        return
    }
    if payload.Action != "submitted" {
        w.WriteHeader(http.StatusNoContent)
        return
    }

    obj := p.analyzeStep().reviewToObject(
        payload.Repo.Owner.Login, payload.Repo.Name, payload.Review)
    _ = p.publishIngestEvent(r.Context(), obj)

    w.WriteHeader(http.StatusAccepted)
}
```

HMAC validation pseudocode:

```go
func validateHMAC(sigHeader, secret string, body io.Reader) bool {
    // Read full body (already consumed above — use io.TeeReader in production)
    // mac = hmac.New(sha256.New, []byte(secret))
    // mac.Write(bodyBytes)
    // expected = "sha256=" + hex.EncodeToString(mac.Sum(nil))
    // return hmac.Equal([]byte(sigHeader), []byte(expected))
    return true // stub
}
```

Design note: never store the webhook secret in plaintext in state.json. Store
the HMAC of it, or keep it only in memory from the env-expanded config value.

---

## 11. Error handling and retry patterns

GitHub APIs are unreliable under network partitions and rate limits. Four
patterns used in this plugin:

### 11a. Rate limit detection

```go
// checkRateLimit inspects response headers and waits if exhausted.
// GitHub sends X-RateLimit-Remaining and X-RateLimit-Reset.
func checkRateLimit(resp *http.Response) error {
    remaining := resp.Header.Get("X-RateLimit-Remaining")
    if remaining == "0" {
        reset := resp.Header.Get("X-RateLimit-Reset")
        // reset is a Unix timestamp. Wait until then + 5s buffer.
        // In production: return a sentinel error; the scheduler retries later.
        return fmt.Errorf("rate limit exhausted; reset at %s", reset)
    }
    return nil
}
```

### 11b. Exponential backoff for transient errors

```go
// retryHTTP calls fn up to maxRetries times with exponential back-off.
// Only retries on 429 Too Many Requests and 5xx responses.
// Returns last error if all attempts fail.
func retryHTTP(fn func() (*http.Response, error), maxRetries int) (*http.Response, error) {
    var last error
    wait := time.Second
    for i := 0; i < maxRetries; i++ {
        resp, err := fn()
        if err != nil {
            last = err
            time.Sleep(wait)
            wait *= 2
            continue
        }
        if resp.StatusCode == 429 || resp.StatusCode >= 500 {
            resp.Body.Close()
            last = fmt.Errorf("HTTP %d", resp.StatusCode)
            time.Sleep(wait)
            wait *= 2
            continue
        }
        return resp, nil
    }
    return nil, last
}
```

### 11c. Partial-success in batch operations

From section 6: per-PR errors are logged and skipped, not propagated. The
cursor advances even if some PRs fail. This prevents a permanently-broken PR
from blocking all future syncs.

### 11d. Idempotency via content hash

The `KnowledgeObject.ContentHash` field prevents duplicate ingestion. Set it
to a deterministic hash of the review ID:

```go
import "crypto/sha256"
import "fmt"

obj.ContentHash = fmt.Sprintf("%x",
    sha256.Sum256([]byte(fmt.Sprintf("github-review-%d", review.ID))))
```

Core will skip ingest if an object with this hash already exists.

---

## 12. State management

`state.go` wraps read/write of `state.json`. State is the plugin's only
persistent storage aside from knowledge objects.

```go
package ghprwatcher

import (
    "encoding/json"
    "os"
    "path/filepath"
    "sync"
)

// stateLock guards concurrent reads/writes from scheduled hook and PostIngest.
var stateLock sync.Mutex

func (p *Plugin) loadState() (map[string]any, error) {
    stateLock.Lock()
    defer stateLock.Unlock()
    path := filepath.Join(p.stateDir, "state.json")
    data, err := os.ReadFile(path)
    if os.IsNotExist(err) {
        return map[string]any{"state_version": "1.0"}, nil
    }
    if err != nil {
        return nil, err
    }
    var m map[string]any
    return m, json.Unmarshal(data, &m)
}

func (p *Plugin) saveState(m map[string]any) error {
    stateLock.Lock()
    defer stateLock.Unlock()
    data, err := json.MarshalIndent(m, "", "  ")
    if err != nil {
        return err
    }
    path := filepath.Join(p.stateDir, "state.json")
    // Write to temp file first; rename is atomic on POSIX.
    tmp := path + ".tmp"
    if err := os.WriteFile(tmp, data, 0o600); err != nil {
        return err
    }
    return os.Rename(tmp, path)
}

// loadCursor returns the Unix timestamp of the last successful sync for a repo.
func (p *Plugin) loadCursor(owner, repo string) int64 {
    state, _ := p.loadState()
    cursors, _ := state["cursors"].(map[string]any)
    if cursors == nil {
        return 0
    }
    key := owner + "/" + repo
    ts, _ := cursors[key].(float64) // JSON numbers decode as float64
    return int64(ts)
}

// storeCursor saves the sync timestamp for a repo.
func (p *Plugin) storeCursor(owner, repo string, ts int64) {
    state, _ := p.loadState()
    cursors, _ := state["cursors"].(map[string]any)
    if cursors == nil {
        cursors = map[string]any{}
    }
    cursors[owner+"/"+repo] = ts
    state["cursors"] = cursors
    _ = p.saveState(state)
}
```

State design rules:

- Always mutex-protect. PostIngest and scheduled hook can fire concurrently.
- Atomic write (write + rename). Prevents corrupt state.json on crash.
- Keep it small. State is not a database. Heavy data (raw PR bodies) goes in
  `cache/`.
- Version the schema (`state_version`). Future migrations can branch on this.

---

## 13. Testing locally

### 13a. Unit test: category classifier

```go
func TestClassifyCategory(t *testing.T) {
    cases := []struct {
        body string
        want string
    }{
        {"Use parameterized queries to prevent SQL injection", "security"},
        {"This causes an N+1 query, add a join", "performance"},
        {"Please use async/await instead of .then()", "code_style"},
        {"Add unit tests for this edge case", "testing"},
        {"Looks good to me", "general"},
    }
    for _, tc := range cases {
        got := classifyCategory(tc.body)
        if got != tc.want {
            t.Errorf("classifyCategory(%q) = %q; want %q", tc.body, got, tc.want)
        }
    }
}
```

### 13b. Integration test: FetchReviews step with mock HTTP

```go
func TestFetchReviews_MockServer(t *testing.T) {
    // 1. Serve fake GitHub API responses from httptest.NewServer.
    // 2. Point tokenTransport.base at the test server transport.
    // 3. Call step.Run with a draft object carrying owner/repo in Metadata.
    // 4. Assert draft.Metadata["reviews"] contains the expected reviews.

    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        switch {
        case strings.Contains(r.URL.Path, "/pulls") && !strings.Contains(r.URL.Path, "/reviews"):
            json.NewEncoder(w).Encode([]map[string]any{
                {"number": 42, "updated_at": time.Now().Format(time.RFC3339)},
            })
        case strings.Contains(r.URL.Path, "/reviews"):
            json.NewEncoder(w).Encode([]ghReview{
                {ID: 1, User: ghUser{Login: "alice"}, Body: "Use async/await",
                    State: "CHANGES_REQUESTED", SubmittedAt: time.Now()},
            })
        }
    }))
    defer srv.Close()

    plugin := &Plugin{cfg: Config{GitHub: GitHubCreds{Token: "test"}}}
    plugin.stateDir = t.TempDir()
    step := &FetchReviews{plugin: plugin}

    // Redirect HTTP to test server by overriding base transport.
    // (Requires exporting baseTransport or using an injection point.)

    draft := &pluginapi.KnowledgeObject{
        Metadata: map[string]any{"owner": "testorg", "repo": "testrepo"},
    }
    out, err := step.Run(context.Background(), draft)
    if err != nil {
        t.Fatal(err)
    }
    reviews := out.Metadata["reviews"].([]ghReview)
    if len(reviews) != 1 || reviews[0].User.Login != "alice" {
        t.Errorf("unexpected reviews: %+v", reviews)
    }
}
```

### 13c. Run a full end-to-end sync manually

```bash
# 1. Export a real token with repo:read scope.
export GITHUB_TOKEN=ghp_...

# 2. Add minimal plugin config to ~/.contexthelp/ctxt.yaml:
#    plugins:
#      github-pr-watcher:
#        enabled: true
#        mode: polling
#        polling_interval_seconds: 60
#        repositories:
#          - owner: myorg
#            repo: my-test-repo
#        github:
#          token: ${GITHUB_TOKEN}

# 3. Start ctxt with the plugin compiled in.
ctxt serve --plugin ./github-pr-watcher/code.so

# 4. Trigger one sync cycle immediately.
ctxt plugin run github-pr-watcher scheduled

# 5. Query resulting insights.
ctxt list type:pr-review-insight --limit 5 --sort created

# 6. Inspect state.json to verify cursor advanced.
cat ~/.contexthelp/plugins/github-pr-watcher/state.json
```

### 13d. Debug tips

- Check plugin logs: `tail -f ~/.contexthelp/plugins/github-pr-watcher/logs/plugin.log`
- Inspect cached raw responses: `ls ~/.contexthelp/plugins/github-pr-watcher/cache/`
- Reset sync cursor: delete `cursors` key from state.json and re-run.
- Verify token scope: `curl -H "Authorization: Bearer $GITHUB_TOKEN" https://api.github.com/user`

---

## 14. Wiring it into ctxt

Compile the plugin and register it with the runtime. Two approaches:

### 14a. Compiled-in (recommended for first-party plugins)

```go
// In cmd/ctxt/main.go or wherever the plugin.Registry is built:

import ghprwatcher "github.com/myorg/github-pr-watcher"

func buildRegistry() *plugin.Registry {
    r := plugin.NewRegistry()
    r.Register(&ghprwatcher.Plugin{})
    // ... other plugins
    return r
}
```

### 14b. Shared library (.so) for third-party plugins

The runtime loads `code.so` from the plugin directory at startup. The shared
library must export a `New() pluginapi.Plugin` symbol:

```go
// In github-pr-watcher/export.go:
package main

import "C"
import (
    ghprwatcher "github.com/myorg/github-pr-watcher"
    "github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
)

//export New
func New() pluginapi.Plugin {
    return &ghprwatcher.Plugin{}
}

func main() {} // required for -buildmode=c-shared
```

Build:

```bash
go build -buildmode=c-shared -o code.so ./github-pr-watcher/
cp code.so ~/.contexthelp/plugins/github-pr-watcher/
```

---

## Summary: design decisions at a glance

| Decision | Choice | Rationale |
|---|---|---|
| Auth | Bearer token via custom transport | Centralised; easy to rotate |
| Polling vs webhook | Config-driven | Polling for laptops; webhook for servers |
| Cursor strategy | Per-repo, advance before analysis | Avoids infinite re-fetch on crash |
| Error strategy | Non-fatal per-PR; batch continues | One bad PR never blocks all repos |
| Fan-out | Publish events per insight | Clean separation; avoids step mutation |
| Idempotency | ContentHash on review ID | Core deduplication prevents double ingest |
| State | Mutex + atomic rename | Safe from concurrent hooks and crashes |
| Testing | Mock HTTP server per step | No real GitHub calls in CI |

---

## See also

- [plugins-api.md](../plugins-api.md) — full API reference
- [plugins-github-pr-review-watcher.md](../plugins-github-pr-review-watcher.md) — full
  production spec (pattern tracking, notification integration, privacy controls)
- [plugins-sample-price-monitor.md](plugins-sample-price-monitor.md) — simpler example
  showing refresh and alert patterns

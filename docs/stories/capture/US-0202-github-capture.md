---
status: shipped
---

# US-0202: GitHub Repository and PR Capture

**System Types:** ctxt
**Personas:** [Knowledge Workers](../../personas/knowledge-workers.md), [Platform Integrators](../../personas/platform-integrators.md), [Researchers & OSINT Analysts](../../personas/knowledge-workers.md)

---

## User Goal

As a developer, I want to capture GitHub repositories, issues, PRs, and profiles into my knowledge base so that code-related knowledge becomes searchable alongside my other content.

---

## Context

GitHub is the central hub for software development knowledge. Architecture decisions live in PR descriptions. Design rationale hides in issue comment threads. API contracts are documented in READMEs that evolve with the codebase. For developers and technical leads, the inability to search across this knowledge alongside meeting notes, design docs, and research papers creates a fragmented understanding of their technical landscape.

The problem is particularly acute for cross-repository work. A developer evaluating whether to adopt a library needs to understand its README, scan recent issues for red flags, and review open PRs for trajectory. Today this requires manually visiting multiple pages, holding context in working memory, and hoping to recall it later. Capturing all of this into a knowledge base makes it searchable, cross-referenceable, and persistent.

GitHub offers two authentication paths: personal access tokens (PATs) for API-level access, and browser cookies for session-level access via the cookie bridge (US-0200). The system supports both. PATs are preferred for automated workflows and provide structured API responses. Cookie-based access is useful for capturing content that requires a logged-in browser view (such as rendered Markdown with private image embeds, or organization-internal repositories). The captured data creates rich entities -- repositories, users, and organizations -- that connect to entities from other platforms, enabling questions like "show me everything related to the authentication service" that span GitHub PRs, Slack discussions, and design docs.

---

## Acceptance Criteria

- [ ] `ctxt capture https://github.com/org/repo` captures README, description, topics, language stats, contributor list
- [ ] `ctxt capture https://github.com/org/repo/pull/123` captures PR: title, description, diff summary, review comments, CI status
- [ ] `ctxt capture https://github.com/org/repo/issues/456` captures issue: title, body, labels, comments, linked PRs
- [ ] `ctxt capture https://github.com/user` captures user profile: bio, pinned repos, contribution graph summary
- [ ] Uses browser cookies OR GitHub personal access token (configurable, PAT preferred when available)
- [ ] Creates entities for repositories (`@github.org/repo`), users (`@github.user`), organizations (`@github.org`)
- [ ] Links captured PRs/issues to their parent repository entity via edges
- [ ] PR diff is summarized (not stored verbatim for large diffs) with key changes highlighted
- [ ] Review comments are preserved with reviewer attribution
- [ ] Issue labels are mapped to tags
- [ ] Processing is async -- returns job ID immediately
- [ ] Handles GitHub rate limiting (5000 req/hr for PAT, lower for cookies) with backoff
- [ ] Private repositories accessible when user has appropriate authentication

---

## Implementation Notes

### CLI Interface

```bash
# Capture a repository
ctxt capture https://github.com/golang/go
{
  "job_id": "j-gh-repo-1a2b3c",
  "object_id": "o-gh-repo-4d5e6f",
  "status": "pending_enrichment",
  "pipeline": "code.github.repo",
  "source": "https://github.com/golang/go",
  "auth": "github_pat"
}

# Capture a pull request
ctxt capture https://github.com/golang/go/pull/12345
{
  "job_id": "j-gh-pr-7g8h9i",
  "object_id": "o-gh-pr-0j1k2l",
  "status": "pending_enrichment",
  "pipeline": "code.github.pr",
  "source": "https://github.com/golang/go/pull/12345",
  "metadata": {
    "pr_state": "open",
    "files_changed": 14,
    "additions": 342,
    "deletions": 89
  }
}

# Capture an issue
ctxt capture https://github.com/golang/go/issues/67890
{
  "job_id": "j-gh-iss-m3n4o5",
  "object_id": "o-gh-iss-p6q7r8",
  "status": "pending_enrichment",
  "pipeline": "code.github.issue",
  "source": "https://github.com/golang/go/issues/67890"
}

# Capture a user profile
ctxt capture https://github.com/torvalds
{
  "job_id": "j-gh-user-s9t0u1",
  "object_id": "o-gh-user-v2w3x4",
  "status": "pending_enrichment",
  "pipeline": "code.github.profile",
  "source": "https://github.com/torvalds"
}

# Configure GitHub authentication
ctxt config set github.token ${GITHUB_TOKEN}
GitHub token configured. Rate limit: 5000 req/hr.

# Check GitHub auth status
ctxt config get github.auth
Auth method: personal_access_token
User: jadb
Rate limit: 4832/5000 remaining (resets in 42 minutes)

# Check job progress
ctxt job get j-gh-pr-7g8h9i
{
  "job_id": "j-gh-pr-7g8h9i",
  "status": "processing",
  "pipeline": "code.github.pr",
  "progress": {
    "current_step": "DiffSummarizer",
    "steps_completed": 3,
    "steps_total": 7,
    "percent": 42
  }
}
```

### REST API

```
POST /api/v1/analyze
Content-Type: application/json

{
  "source_type": "url",
  "source_url": "https://github.com/golang/go/pull/12345",
  "auth_method": "github_pat",
  "options": {
    "capture_comments": true,
    "capture_diff": true,
    "summarize_diff": true
  }
}

-> 202 Accepted
{
  "job_id": "j-gh-pr-7g8h9i",
  "object_id": "o-gh-pr-0j1k2l",
  "status": "pending_enrichment",
  "pipeline": "code.github.pr"
}
```

Retrieving a captured PR:

```
GET /api/v1/objects/o-gh-pr-0j1k2l

-> 200 OK
{
  "id": "o-gh-pr-0j1k2l",
  "type": "code",
  "subtype": "github.pr",
  "raw_content": "## Summary\nRefactor the HTTP router to use radix tree...",
  "source": {
    "type": "url",
    "url": "https://github.com/golang/go/pull/12345",
    "captured_at": "2026-02-18T10:30:45Z",
    "auth_method": "github_pat"
  },
  "metadata": {
    "platform": "github.com",
    "owner": "golang",
    "repo": "go",
    "pr_number": 12345,
    "pr_state": "open",
    "pr_title": "net/http: refactor router to use radix tree",
    "author": "bradfitz",
    "created_at": "2026-02-15T09:00:00Z",
    "updated_at": "2026-02-17T14:30:00Z",
    "files_changed": 14,
    "additions": 342,
    "deletions": 89,
    "labels": ["Performance", "net/http"],
    "reviewers": ["rsc", "ianlancetaylor"],
    "ci_status": "passing",
    "merge_status": "mergeable"
  },
  "sections": [
    {
      "id": "sec-description",
      "title": "PR Description",
      "content": "This PR refactors the HTTP router from a linear scan to a radix tree..."
    },
    {
      "id": "sec-diff-summary",
      "title": "Diff Summary",
      "content": "Key changes:\n- New file: net/http/radix.go (radix tree implementation)\n- Modified: net/http/server.go (router integration)\n- Removed: net/http/linear_router.go\n- Benchmark improvement: 3.2x faster route matching for 100+ routes"
    },
    {
      "id": "sec-review-001",
      "title": "Review: rsc",
      "content": "This looks good overall. One concern about memory allocation in the radix node split...",
      "metadata": {"reviewer": "rsc", "state": "changes_requested", "submitted_at": "2026-02-16T11:00:00Z"}
    },
    {
      "id": "sec-review-002",
      "title": "Review: ianlancetaylor",
      "content": "LGTM with minor nits. The benchmark numbers are impressive.",
      "metadata": {"reviewer": "ianlancetaylor", "state": "approved", "submitted_at": "2026-02-17T14:30:00Z"}
    }
  ],
  "tags": ["performance", "net-http", "refactoring", "go"],
  "mentions": [
    {"entity": "@github.golang/go"},
    {"entity": "@github.bradfitz"},
    {"entity": "@github.rsc"}
  ]
}
```

### Pipeline Steps

**`code.github.repo`** (repository capture):

```
CookieFetcher -> GitHubRepoParser -> ReadmeExtractor -> EntityResolver -> Sectioner -> Tagger -> EmbeddingGenerator
```

**`code.github.pr`** (pull request capture):

```
CookieFetcher -> GitHubPRParser -> DiffSummarizer -> EntityResolver -> Sectioner -> Tagger -> EmbeddingGenerator
```

**`code.github.issue`** (issue capture):

```
CookieFetcher -> GitHubIssueParser -> EntityResolver -> Sectioner -> Tagger -> EmbeddingGenerator
```

**`code.github.profile`** (user profile capture):

```
CookieFetcher -> GitHubProfileParser -> EntityResolver -> Sectioner -> Tagger -> EmbeddingGenerator
```

Step responsibilities:

| Step | Input | Output |
|------|-------|--------|
| CookieFetcher | URL + auth config | Authenticated HTTP client (PAT header or session cookies) |
| GitHubRepoParser | Repo page/API response | Structured repo data: description, topics, languages, contributors, stats |
| GitHubPRParser | PR page/API response | Structured PR data: title, body, diff stat, reviews, comments, CI status |
| GitHubIssueParser | Issue page/API response | Structured issue data: title, body, labels, comments, linked PRs, timeline |
| GitHubProfileParser | Profile page/API response | Structured profile: bio, pinned repos, contribution summary, org memberships |
| ReadmeExtractor | Repo API response | README content parsed from Markdown to structured sections |
| DiffSummarizer | PR diff data | AI-generated summary of key changes, file-level change descriptions |
| EntityResolver | Structured data | Creates/links entities for repo, users, orgs; resolves to canonical entities |
| Sectioner | Parsed content | Structured Sections (description, diff, reviews, comments) |
| Tagger | Sections + full text | Tags from vocabulary + GitHub labels mapped to tags |
| EmbeddingGenerator | Sections + full text | Embeddings per section + full-document embedding |

### GitHub Authentication

```go
type GitHubAuth struct {
    // Preferred: Personal Access Token
    Token string

    // Fallback: Cookie bridge
    CookieStore *CookieStore
}

func (a *GitHubAuth) HTTPClient() *http.Client {
    if a.Token != "" {
        return &http.Client{
            Transport: &tokenTransport{
                token: a.Token,
                base:  http.DefaultTransport,
            },
        }
    }

    // Fall back to cookie-based auth
    cookies, err := a.CookieStore.Get("github.com")
    if err == nil && len(cookies) > 0 {
        jar, _ := cookiejar.New(nil)
        jar.SetCookies(
            &url.URL{Scheme: "https", Host: "github.com"},
            cookies,
        )
        return &http.Client{Jar: jar}
    }

    // Unauthenticated (lower rate limits, no private repos)
    return http.DefaultClient
}

type tokenTransport struct {
    token string
    base  http.RoundTripper
}

func (t *tokenTransport) RoundTrip(req *http.Request) (*http.Response, error) {
    req.Header.Set("Authorization", "Bearer "+t.token)
    req.Header.Set("Accept", "application/vnd.github.v3+json")
    return t.base.RoundTrip(req)
}
```

### Backend Processing

1. User provides GitHub URL via CLI (`ctxt capture <url>`) or REST API
2. URL pattern determines pipeline:
   - `github.com/org/repo` -> `code.github.repo`
   - `github.com/org/repo/pull/N` -> `code.github.pr`
   - `github.com/org/repo/issues/N` -> `code.github.issue`
   - `github.com/user` -> `code.github.profile`
3. CookieFetcher selects auth method: PAT (if configured) > cookie bridge > unauthenticated
4. For repositories: fetches repo metadata via GitHub API, downloads README, extracts topic list, language breakdown, and top contributors
5. For PRs: fetches PR metadata, description (rendered Markdown), diff stat, file list, review comments, and CI status via API
6. DiffSummarizer (PR only) sends the diff to the configured AI provider to generate a human-readable summary of key changes; for large diffs (>10,000 lines), summarizes per-file changes rather than the full diff
7. For issues: fetches issue metadata, body, labels, full comment timeline, and linked PRs/commits
8. EntityResolver creates entities:
   - Repository entity: `@github.org/repo` with description, topics, star count
   - User entity: `@github.username` with profile data
   - Organization entity: `@github.org` with org metadata
9. Resolved entities linked to canonical entities if previously captured from other platforms
10. PRs and issues linked to parent repository entity via `belongs_to` edges
11. Sectioner creates structured Sections: description, diff summary, individual reviews, comments
12. GitHub labels are mapped to tags (e.g., label "bug" -> tag "bug")
13. Tagger assigns additional tags based on content analysis
14. EmbeddingGenerator creates embeddings for search
15. Job status updated to `completed`

### GitHub URL Pattern Detection

```go
type GitHubURLParser struct{}

func (p *GitHubURLParser) Parse(rawURL string) (*GitHubCapture, error) {
    u, err := url.Parse(rawURL)
    if err != nil {
        return nil, err
    }

    if u.Hostname() != "github.com" {
        return nil, fmt.Errorf("not a GitHub URL: %s", u.Hostname())
    }

    parts := strings.Split(strings.Trim(u.Path, "/"), "/")

    switch {
    case len(parts) == 1:
        // github.com/user -> profile
        return &GitHubCapture{Type: "profile", User: parts[0]}, nil
    case len(parts) == 2:
        // github.com/org/repo -> repository
        return &GitHubCapture{Type: "repo", Owner: parts[0], Repo: parts[1]}, nil
    case len(parts) == 4 && parts[2] == "pull":
        // github.com/org/repo/pull/123 -> PR
        num, _ := strconv.Atoi(parts[3])
        return &GitHubCapture{Type: "pr", Owner: parts[0], Repo: parts[1], Number: num}, nil
    case len(parts) == 4 && parts[2] == "issues":
        // github.com/org/repo/issues/456 -> issue
        num, _ := strconv.Atoi(parts[3])
        return &GitHubCapture{Type: "issue", Owner: parts[0], Repo: parts[1], Number: num}, nil
    default:
        return nil, fmt.Errorf("unrecognized GitHub URL pattern: %s", u.Path)
    }
}
```

### Configuration

```yaml
# In configuration.yaml
github:
  # Personal access token (preferred auth method)
  token: ${GITHUB_TOKEN}

  # Fallback to cookie bridge if no token
  fallbackToCookies: true

  # Rate limiting
  rateLimit:
    # PAT rate limit awareness
    reservePercent: 10  # Keep 10% of rate limit in reserve
    backoffOnLimit: true
    maxRetries: 5

  # Repository capture settings
  repo:
    # Capture README content
    captureReadme: true
    # Capture contributor list (top N)
    topContributors: 20
    # Capture language breakdown
    captureLanguages: true
    # Capture recent releases
    captureReleases: 5

  # PR capture settings
  pr:
    # Capture review comments
    captureReviews: true
    # Capture inline diff comments
    captureDiffComments: true
    # Summarize diff with AI
    summarizeDiff: true
    # Max diff size (lines) before switching to per-file summary
    maxDiffLines: 10000
    # Capture CI/check status
    captureCIStatus: true

  # Issue capture settings
  issue:
    # Capture all comments
    captureComments: true
    # Maximum comments to capture
    maxComments: 200
    # Capture linked PRs
    captureLinkedPRs: true
    # Capture issue timeline events
    captureTimeline: true

  # Profile capture settings
  profile:
    # Capture pinned repositories
    capturePinnedRepos: true
    # Capture contribution graph summary
    captureContributions: true
    # Capture organization memberships (if visible)
    captureOrgs: true
```

---

## E2E Test Checklist

- [ ] CLI: `ctxt capture https://github.com/org/repo` returns job ID within 2 seconds
- [ ] CLI: `ctxt capture https://github.com/org/repo/pull/123` returns job ID within 2 seconds
- [ ] CLI: `ctxt capture https://github.com/org/repo/issues/456` returns job ID within 2 seconds
- [ ] CLI: `ctxt capture https://github.com/user` returns job ID within 2 seconds
- [ ] CLI: `ctxt config set github.token <token>` stores the token and subsequent captures include PAT auth (verifiable by rate limit reported as 5000 req/hr)
- [ ] Repo: README content captured and decomposed into sections
- [ ] Repo: Topics, language stats, and contributor list captured in metadata
- [ ] Repo: Repository entity created with `@github.org/repo` identifier
- [ ] PR: Title, description, diff summary, and review comments captured
- [ ] PR: DiffSummarizer generates readable summary of changes
- [ ] PR: Review comments attributed to reviewers with approval state
- [ ] PR: CI/check status captured in metadata
- [ ] PR: Linked to parent repository entity via `belongs_to` edge
- [ ] Issue: Title, body, labels, and comments captured
- [ ] Issue: Labels mapped to tags correctly
- [ ] Issue: Linked PRs referenced in metadata
- [ ] Issue: Linked to parent repository entity via `belongs_to` edge
- [ ] Profile: Bio, pinned repos, and contribution summary captured
- [ ] Auth: PAT authentication works (higher rate limit, private repo access)
- [ ] Auth: Cookie bridge fallback works when no PAT configured
- [ ] Auth: Unauthenticated fallback works for public repos (lower rate limit)
- [ ] Auth: Private repository accessible with valid PAT
- [ ] Auth: Private repository returns clear auth error without valid credentials
- [ ] Rate Limit: Rate-limited response (HTTP 403/429) triggers backoff and retry
- [ ] Rate Limit: Rate limit reserve prevents exhausting the full allocation
- [ ] Entity: Repository, user, and org entities created correctly
- [ ] Entity: Same user captured twice resolves to same entity (idempotent)
- [ ] REST API: `POST /api/v1/analyze` request payload for a PR contains `source_type: url`, `source_url`, `auth_method: github_pat`, and `options` object with `capture_comments`, `capture_diff`, and `summarize_diff` fields
- [ ] REST API: `POST /api/v1/analyze` returns 202 with `job_id` and `object_id`
- [ ] Storage: Completed PR object stored with `source.auth_method: github_pat`, `metadata.pr_number`, `metadata.files_changed`, `metadata.reviewers`, and `metadata.ci_status` (verifiable via `GET /api/v1/objects/<id>`)
- [ ] Search: PR description searchable via `ctxt search "radix tree router"`
- [ ] Search: Issue comments searchable via `ctxt search "race condition in handler"`
- [ ] Error: Invalid GitHub URL returns descriptive error
- [ ] Error: Non-existent repo/PR/issue returns 404-based error message
- [ ] Pipeline: All four pipelines complete their steps in correct order

---

## Related Stories

- [US-0200](./US-0200-browser-cookie-bridge.md) -- Cookie bridge provides fallback authentication for GitHub
- [US-0002](../ingestion/US-0002-url-capture-and-extraction.md) -- Base URL capture; GitHub capture extends with platform-specific parsing
- [US-0006](../ingestion/US-0006-document-parsing-and-decomposition.md) -- README processing leverages document parsing pipeline
- [US-0009](../enrichment/US-0009-extract-entities-and-mentions.md) -- Entity extraction from PR/issue content
- [US-0013](../enrichment/US-0013-detect-and-extract-code-snippets.md) -- Code snippet extraction from PR diffs
- [US-0050](../enrichment/US-0050-extract-code-metrics-and-complexity.md) -- Code metrics from captured repository content
- [US-0201](./US-0201-x-twitter-capture.md) -- Similar platform-specific capture pattern for X/Twitter

---

## Personas

- [Knowledge Workers](../../personas/knowledge-workers.md)
- Solo Developer
- Automation Builder

---

## E2E Tests

- `test/integration/us0202_github_capture_test.go::TestUS0202_RepoTitleDescriptionStored`
- `test/integration/us0202_github_capture_test.go::TestUS0202_IssueTitleBodyLabelsStored`
- `test/integration/us0202_github_capture_test.go::TestUS0202_PRTitleBodyStored`

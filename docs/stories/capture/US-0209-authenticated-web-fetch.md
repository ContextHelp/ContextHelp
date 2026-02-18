# US-0209: Authenticated Web Content Fetch

**System Types:** ctxt, dpkms (self-hosted)
**Personas:** [Knowledge Workers](../../personas/knowledge-workers.md), [Researchers & OSINT Analysts](../../personas/researchers-osint.md)

---

## User Goal

As a knowledge worker, I want to capture content from websites that require authentication (paywalled articles, internal wikis, private dashboards) using my existing browser session so that walled-off knowledge becomes part of my searchable knowledge base.

---

## Context

A significant portion of valuable knowledge lives behind authentication walls: premium articles on Medium and Substack, internal wikis on Confluence and Notion, private dashboards and reports, Slack message archives, and gated research papers. Standard URL capture fails for these resources because the fetch request lacks the authentication credentials that the user's browser already possesses. The result is either an error page, a login redirect, or a stripped-down preview instead of the full content.

Authenticated web fetch solves this by leveraging the user's existing browser session. When `--auth browser` is specified, the system injects stored cookies from the user's browser into the fetch request, impersonating their authenticated session. This approach avoids storing passwords or API keys for every service -- the user simply needs to be logged in via their browser, and ctxt piggybacks on that session. A cookie freshness check warns when cookies are stale (beyond a configurable age), prompting the user to refresh their browser session before capture.

The system distinguishes between public and private content for robots.txt compliance: public sites respect robots.txt directives, while authenticated private content (where the user has legitimate access) bypasses these restrictions. Per-domain configuration allows fine-grained control over authentication method, rate limiting, and content extraction strategy. Readability extraction strips navigation, ads, and page chrome to preserve only the article body, ensuring clean content regardless of the source site's layout complexity.

---

## Acceptance Criteria

- [ ] `ctxt capture <url> --auth browser` fetches content using stored browser cookies
- [ ] System falls back to unauthenticated fetch when cookies are missing or expired
- [ ] Cookie freshness check warns when cookies are older than the configured `cookieMaxAge`
- [ ] Readability extraction strips navigation, ads, and page chrome from captured content
- [ ] Authentication method is recorded in object metadata (`cookie`, `public`, or `api-key`)
- [ ] Per-domain configuration controls auth method, rate limits, and extraction strategy
- [ ] Rate limiting is enforced per domain to avoid triggering anti-bot measures
- [ ] Public sites respect robots.txt; authenticated private content bypasses robots.txt
- [ ] Captured content is searchable within 30 seconds of capture completion
- [ ] Failed fetches (expired cookies, access denied) return clear error messages with remediation steps
- [ ] System supports cookie extraction from Chrome, Firefox, and Safari browsers
- [ ] Configuration supports wildcard domain patterns (e.g., `*.company.com`)

---

## Implementation Notes

### CLI Interface

```bash
# Capture a paywalled article using browser cookies
ctxt capture https://medium.com/@author/premium-article --auth browser
# -> Fetching with browser cookies for medium.com...
# -> Content captured successfully
{
  "job_id": "j-auth-5a6b7c",
  "object_id": "o-auth-d8e9f0",
  "status": "pending_enrichment",
  "pipeline": "web.authenticated",
  "auth_method": "cookie",
  "source": "https://medium.com/@author/premium-article"
}

# Capture internal wiki page
ctxt capture https://internal-wiki.company.com/page --auth browser
# -> Fetching with browser cookies for *.company.com...

# Capture with explicit auth method
ctxt capture https://api.example.com/report --auth api-key --api-key-header "X-Api-Key"
{
  "job_id": "j-auth-1c2d3e",
  "object_id": "o-auth-4f5g6h",
  "status": "pending_enrichment",
  "pipeline": "web.authenticated",
  "auth_method": "api-key"
}

# Capture with unauthenticated fetch (default)
ctxt capture https://blog.example.com/public-post
{
  "job_id": "j-pub-7i8j9k",
  "object_id": "o-pub-0l1m2n",
  "status": "pending_enrichment",
  "pipeline": "web.authenticated",
  "auth_method": "none"
}

# Check cookie freshness for a domain
ctxt capture cookies --check medium.com
# -> medium.com: cookies found (age: 2d 4h, max: 7d) - FRESH
# -> linkedin.com: cookies found (age: 9d 1h, max: 7d) - STALE (please refresh browser session)
# -> internal.company.com: no cookies found

# List configured domains
ctxt capture domains
# -> Domain              Auth       Rate Limit  Robots.txt
# -> medium.com          browser    2/min       respected
# -> *.company.com       browser    10/min      bypassed
# -> arxiv.org           none       5/min       respected
```

### REST API

```
# Authenticated capture
POST /api/v1/capture/url
Content-Type: application/json

{
  "url": "https://medium.com/@author/premium-article",
  "auth": "browser",
  "profile": "research"
}

-> 202 Accepted
{
  "job_id": "j-auth-5a6b7c",
  "object_id": "o-auth-d8e9f0",
  "status": "pending_enrichment",
  "pipeline": "web.authenticated",
  "auth_method": "cookie",
  "cookie_age": "2d 4h",
  "cookie_fresh": true
}

# Capture with API key auth
POST /api/v1/capture/url
Content-Type: application/json

{
  "url": "https://api.example.com/report",
  "auth": "api-key",
  "auth_config": {
    "header": "X-Api-Key",
    "secret_ref": "example-api-key"
  }
}

-> 202 Accepted
{
  "job_id": "j-auth-1c2d3e",
  "object_id": "o-auth-4f5g6h",
  "status": "pending_enrichment",
  "pipeline": "web.authenticated",
  "auth_method": "api-key"
}

# Check cookie freshness
GET /api/v1/capture/cookies?domain=medium.com

-> 200 OK
{
  "domain": "medium.com",
  "cookies_found": true,
  "cookie_age": "2d 4h",
  "max_age": "7d",
  "fresh": true,
  "browsers_checked": ["chrome", "firefox"]
}

# List domain configurations
GET /api/v1/capture/domains

-> 200 OK
{
  "domains": [
    {
      "pattern": "medium.com",
      "auth": "browser",
      "rate_limit": "2/min",
      "robots_txt": "respected"
    },
    ...
  ]
}
```

### Pipeline Steps

**web.authenticated** (authenticated fetch pipeline):
```
CookieInjector -> HTMLFetcher -> ReadabilityConverter -> MetadataExtractor -> Sectioner -> Tagger -> EmbeddingGenerator
```

Each step implements the `PipelineStep` interface:

```go
type CookieInjectorStep struct {
    cookieStore   CookieStore
    domainConfig  DomainConfigStore
}

func (s *CookieInjectorStep) Name() string { return "cookie_injector" }

func (s *CookieInjectorStep) Run(ctx context.Context,
    draft *KnowledgeObject) (*KnowledgeObject, error) {

    targetURL := draft.Source.URL
    domain := extractDomain(targetURL)

    // Look up domain configuration
    config, err := s.domainConfig.Match(ctx, domain)
    if err != nil {
        config = DefaultDomainConfig() // Fall back to defaults
    }

    authMethod := config.Auth
    if override, ok := draft.Metadata["auth_override"].(string); ok {
        authMethod = override
    }

    switch authMethod {
    case "browser":
        cookies, err := s.cookieStore.GetForDomain(ctx, domain)
        if err != nil || len(cookies) == 0 {
            // Fall back to unauthenticated
            draft.Metadata["auth_method"] = "none"
            draft.Metadata["auth_fallback"] = true
            draft.Metadata["auth_fallback_reason"] = "no cookies found for domain"
            return draft, nil
        }

        // Check freshness
        oldestCookie := findOldestCookie(cookies)
        cookieAge := time.Since(oldestCookie.Created)
        maxAge := parseDuration(config.CookieMaxAge)

        if cookieAge > maxAge {
            draft.Metadata["cookie_stale"] = true
            draft.Metadata["cookie_age"] = cookieAge.String()
            // Still use cookies, but warn
        }

        draft.Metadata["auth_method"] = "cookie"
        draft.Metadata["cookies"] = cookies
        draft.Metadata["cookie_count"] = len(cookies)

    case "api-key":
        draft.Metadata["auth_method"] = "api-key"
        draft.Metadata["auth_header"] = config.APIKeyHeader

    default:
        draft.Metadata["auth_method"] = "none"
    }

    draft.Metadata["rate_limit"] = config.RateLimit
    draft.Metadata["respect_robots_txt"] = config.RespectRobotsTxt

    return draft, nil
}
```

```go
type HTMLFetcherStep struct {
    httpClient   *http.Client
    rateLimiter  RateLimiter
    robotsCache  RobotsCache
}

func (s *HTMLFetcherStep) Name() string { return "html_fetcher" }

func (s *HTMLFetcherStep) Run(ctx context.Context,
    draft *KnowledgeObject) (*KnowledgeObject, error) {

    targetURL := draft.Source.URL
    domain := extractDomain(targetURL)

    // Rate limiting
    rateLimit := draft.Metadata["rate_limit"].(string)
    if err := s.rateLimiter.Wait(ctx, domain, rateLimit); err != nil {
        return nil, fmt.Errorf("html_fetcher: rate limit exceeded for %s: %w", domain, err)
    }

    // Robots.txt check (only for public/unauthenticated)
    respectRobots := draft.Metadata["respect_robots_txt"].(bool)
    authMethod := draft.Metadata["auth_method"].(string)

    if respectRobots && authMethod == "none" {
        allowed, err := s.robotsCache.IsAllowed(ctx, targetURL)
        if err == nil && !allowed {
            return nil, fmt.Errorf("html_fetcher: URL blocked by robots.txt: %s", targetURL)
        }
    }

    // Build request
    req, err := http.NewRequestWithContext(ctx, "GET", targetURL, nil)
    if err != nil {
        return nil, fmt.Errorf("html_fetcher: build request failed: %w", err)
    }

    req.Header.Set("User-Agent", "ctxt/1.0 (knowledge capture)")

    // Inject authentication
    switch authMethod {
    case "cookie":
        cookies := draft.Metadata["cookies"].([]http.Cookie)
        for _, c := range cookies {
            req.AddCookie(&c)
        }
    case "api-key":
        header := draft.Metadata["auth_header"].(string)
        key, _ := secrets.Get(ctx, draft.Metadata["auth_secret_ref"].(string))
        req.Header.Set(header, key)
    }

    resp, err := s.httpClient.Do(req)
    if err != nil {
        return nil, fmt.Errorf("html_fetcher: request failed: %w", err)
    }
    defer resp.Body.Close()

    if resp.StatusCode == 401 || resp.StatusCode == 403 {
        return nil, fmt.Errorf("html_fetcher: access denied (HTTP %d) - cookies may be expired", resp.StatusCode)
    }

    if resp.StatusCode != 200 {
        return nil, fmt.Errorf("html_fetcher: unexpected status %d for %s", resp.StatusCode, targetURL)
    }

    body, err := io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024)) // 10MB limit
    if err != nil {
        return nil, fmt.Errorf("html_fetcher: read body failed: %w", err)
    }

    draft.RawContent = body
    draft.ContentType = resp.Header.Get("Content-Type")
    draft.Metadata["http_status"] = resp.StatusCode
    draft.Metadata["content_length"] = len(body)
    draft.Metadata["final_url"] = resp.Request.URL.String() // After redirects

    return draft, nil
}
```

```go
type ReadabilityConverterStep struct{}

func (s *ReadabilityConverterStep) Name() string { return "readability_converter" }

func (s *ReadabilityConverterStep) Run(ctx context.Context,
    draft *KnowledgeObject) (*KnowledgeObject, error) {

    // Apply readability extraction (similar to Firefox Reader View)
    article, err := readability.Extract(bytes.NewReader(draft.RawContent), draft.Source.URL)
    if err != nil {
        // Fall back to basic HTML text extraction
        text := extractTextFromHTML(draft.RawContent)
        draft.Sections = append(draft.Sections, Section{
            Title:   "Content",
            Content: text,
        })
        draft.Metadata["readability_fallback"] = true
        return draft, nil
    }

    draft.Sections = append(draft.Sections, Section{
        Title:   article.Title,
        Content: article.TextContent,
    })

    draft.Metadata["article_title"] = article.Title
    draft.Metadata["article_byline"] = article.Byline
    draft.Metadata["article_excerpt"] = article.Excerpt
    draft.Metadata["article_length"] = article.Length
    draft.Metadata["article_site_name"] = article.SiteName

    return draft, nil
}
```

### Backend Processing

1. User invokes `ctxt capture <url> --auth browser` or the equivalent REST API call
2. System looks up domain configuration to determine auth method, rate limits, and robots.txt policy
3. `CookieInjector` extracts cookies from the user's browser for the target domain
4. If cookies are missing, system falls back to unauthenticated fetch with a warning
5. If cookies are present but stale (older than `cookieMaxAge`), a warning is emitted
6. `HTMLFetcher` enforces rate limiting per domain before making the request
7. For public unauthenticated requests, robots.txt is checked; blocked URLs are rejected
8. For authenticated requests to private content, robots.txt is bypassed
9. `HTMLFetcher` makes the HTTP request with injected cookies or API key header
10. If the response is 401/403, an error is returned suggesting cookie refresh
11. `ReadabilityConverter` strips page chrome (nav, ads, sidebars) and extracts article body
12. `MetadataExtractor` records URL, title, byline, site name, and auth method
13. `Sectioner` splits long articles into logical sections
14. `Tagger` and `EmbeddingGenerator` complete enrichment
15. The enriched KnowledgeObject is stored with full provenance metadata

### Cookie Extraction

```go
type CookieStore interface {
    // GetForDomain returns cookies for a domain from the configured browser(s)
    GetForDomain(ctx context.Context, domain string) ([]http.Cookie, error)
    // ListBrowsers returns available browser cookie stores
    ListBrowsers(ctx context.Context) ([]BrowserInfo, error)
    // CheckFreshness returns cookie age for a domain
    CheckFreshness(ctx context.Context, domain string) (*FreshnessInfo, error)
}

type BrowserInfo struct {
    Name     string // chrome, firefox, safari
    Profile  string // default, work, personal
    CookieDB string // path to cookie database
}

type FreshnessInfo struct {
    Domain      string
    Found       bool
    CookieCount int
    OldestAge   time.Duration
    NewestAge   time.Duration
    MaxAge      time.Duration
    Fresh       bool
}

// Chrome cookie extraction (SQLite database, AES-encrypted on macOS)
type ChromeCookieExtractor struct {
    profilePath string
    keychain    KeychainAccess // macOS Keychain for decryption key
}

// Firefox cookie extraction (SQLite database, unencrypted)
type FirefoxCookieExtractor struct {
    profilePath string
}

// Safari cookie extraction (binary plist format)
type SafariCookieExtractor struct {
    cookiePath string
}
```

### Configuration

```yaml
# In configuration.yaml
pipelines:
  web.authenticated:
    steps:
      - cookie_injector
      - html_fetcher
      - readability_converter
      - metadata_extractor
      - sectioner
      - tagger
      - embedding_generator

capture:
  domains:
    "medium.com":
      auth: browser
      rateLimit: 2/min
      respectRobotsTxt: true
    "substack.com":
      auth: browser
      rateLimit: 3/min
      respectRobotsTxt: true
    "*.company.com":
      auth: browser
      rateLimit: 10/min
      respectRobotsTxt: false      # Internal content
    "arxiv.org":
      auth: none
      rateLimit: 5/min
      respectRobotsTxt: true
    "api.example.com":
      auth: api-key
      apiKeyHeader: "X-Api-Key"
      apiKeySecretRef: "example-api-key"
      rateLimit: 10/min
      respectRobotsTxt: false

  defaultAuth: none
  cookieMaxAge: 7d                   # Warn when cookies older than this
  respectRobotsTxt: true             # Default for unconfigured domains
  maxResponseSize: 10485760          # 10MB max response body

  browsers:
    chrome:
      enabled: true
      profile: default               # Chrome profile to use
    firefox:
      enabled: true
      profile: default
    safari:
      enabled: false                  # Disabled by default (complex extraction)

  fetch:
    timeout: 30s                      # HTTP request timeout
    followRedirects: true
    maxRedirects: 5
    userAgent: "ctxt/1.0 (knowledge capture)"
    retryOn5xx: true
    retryAttempts: 2
    retryBackoff: 5s
```

### Knowledge Object Structure

```json
{
  "id": "o-auth-d8e9f0",
  "type": "web_article",
  "subtype": "authenticated",
  "content_type": "text/html",
  "metadata": {
    "auth_method": "cookie",
    "cookie_count": 12,
    "cookie_stale": false,
    "http_status": 200,
    "content_length": 45230,
    "final_url": "https://medium.com/@author/premium-article-abc123",
    "article_title": "Understanding Distributed Consensus",
    "article_byline": "Jane Doe",
    "article_excerpt": "A deep dive into Raft, Paxos, and modern consensus algorithms...",
    "article_length": 8420,
    "article_site_name": "Medium"
  },
  "sections": [
    {
      "title": "Understanding Distributed Consensus",
      "content": "A deep dive into Raft, Paxos, and modern consensus algorithms..."
    }
  ],
  "tags": ["distributed-systems", "consensus", "raft", "paxos"],
  "source": {
    "url": "https://medium.com/@author/premium-article",
    "title": "Understanding Distributed Consensus",
    "ingested_at": "2026-02-18T10:30:00Z",
    "capture_method": "authenticated_fetch"
  },
  "pipeline": {
    "name": "web.authenticated",
    "steps_completed": ["cookie_injector", "html_fetcher", "readability_converter", "metadata_extractor", "sectioner", "tagger", "embedding_generator"],
    "completed_at": "2026-02-18T10:30:12Z"
  }
}
```

---

## E2E Test Checklist

- [ ] CLI: `ctxt capture <url> --auth browser` fetches content using browser cookies and returns job ID
- [ ] CLI: Capture without `--auth` defaults to unauthenticated fetch
- [ ] CLI: `ctxt capture cookies --check <domain>` reports cookie freshness accurately
- [ ] CLI: `ctxt capture domains` lists all configured domain patterns
- [ ] Auth: Browser cookies are successfully extracted from Chrome cookie store
- [ ] Auth: Browser cookies are successfully extracted from Firefox cookie store
- [ ] Auth: Missing cookies trigger fallback to unauthenticated fetch with warning
- [ ] Auth: Stale cookies (beyond `cookieMaxAge`) trigger warning but still attempt fetch
- [ ] Auth: API key auth injects the correct header with secret value
- [ ] Auth: Authentication method is recorded in object metadata
- [ ] Readability: Navigation, ads, and page chrome are stripped from captured content
- [ ] Readability: Article title, byline, and excerpt are extracted and stored
- [ ] Readability: Fallback to basic HTML text extraction when readability fails
- [ ] Robots.txt: Public unauthenticated URLs respect robots.txt directives
- [ ] Robots.txt: Authenticated private content bypasses robots.txt
- [ ] Rate Limiting: Requests respect per-domain rate limits
- [ ] Rate Limiting: Default rate limit applies to unconfigured domains
- [ ] Domain Config: Wildcard patterns (e.g., `*.company.com`) match subdomains correctly
- [ ] Error: HTTP 401/403 returns clear error suggesting cookie refresh
- [ ] Error: HTTP 404 returns descriptive error without crash
- [ ] Error: Timeout (> 30s) is handled gracefully with error message
- [ ] Error: Response exceeding `maxResponseSize` is rejected
- [ ] REST API: `POST /api/v1/capture/url` with auth returns 202 + job ID
- [ ] REST API: `GET /api/v1/capture/cookies?domain=X` returns freshness info
- [ ] REST API: `GET /api/v1/capture/domains` returns domain configurations
- [ ] Search: Captured authenticated content is searchable via `ctxt search`
- [ ] Async: Capture returns immediately; enrichment completes within 30 seconds
- [ ] Resilience: 5xx responses are retried up to `retryAttempts` times with backoff
- [ ] Resilience: Worker crash during pipeline causes automatic job retry

---

## Related Stories

- [US-0207](./US-0207-web-tab-capture.md) -- Browser tab capture (extension uses same auth mechanism)
- [US-0208](./US-0208-temporal-watch.md) -- Temporal watches need authenticated fetch for monitored URLs
- [US-0002](../ingestion/US-0002-url-capture-and-extraction.md) -- URL capture base functionality
- [US-0031](../admin/US-0031-configure-encryption-and-secrets.md) -- Secrets management for API keys
- [US-0012](../enrichment/US-0012-generate-summaries-and-sections.md) -- Summarization of captured articles
- [US-0011](../enrichment/US-0011-assign-tags-from-vocabulary.md) -- Tag assignment from extracted content

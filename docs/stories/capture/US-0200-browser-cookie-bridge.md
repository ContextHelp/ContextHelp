# US-0200: Browser Cookie Bridge

**System Types:** ctxt
**Personas:** [Knowledge Workers](../../personas/knowledge-workers.md), [Researchers & OSINT Analysts](../../personas/knowledge-workers.md)

---

## User Goal

As a knowledge worker, I want a browser extension that securely shares my authenticated session cookies with ctxt so that I can capture content from any website I'm logged into without re-authenticating or using API keys.

---

## Context

The modern web is increasingly gated behind authentication. Paywalled articles, private repositories, social media feeds, internal wikis, SaaS dashboards -- the content a knowledge worker needs to capture lives behind login walls. Without a mechanism to share browser session state with ctxt, users face an impossible choice: manually copy-paste content (losing structure and provenance) or configure bespoke API keys for every platform (impractical for dozens of services).

The browser cookie bridge solves this by letting users share their existing authenticated sessions. The browser extension detects when the user is logged into a configured domain, exports the relevant session cookies, encrypts them, and stores them in ctxt's local cookie store. When ctxt needs to fetch content from that domain (either via explicit capture or as part of a pipeline), it injects the stored cookies into its HTTP client, appearing as the user's authenticated browser session.

Security is paramount. Cookies are encrypted at rest with AES-256-GCM, scoped strictly to their originating domain (never sent cross-domain), and automatically expire. The extension communicates with ctxt's local daemon over a localhost-only WebSocket connection, never transmitting cookies over the network. Users retain full control: they can list stored domains, clear cookies selectively, and configure which domains are allowed. The extension also provides direct capture capabilities -- a "Capture this page" button, right-click context menu for selected text or elements, and keyboard shortcuts -- turning the browser into a first-class ctxt input surface.

---

## Acceptance Criteria

- [ ] Browser extension available for Chrome, Firefox, and Safari
- [ ] Extension exports cookies for configured domains to ctxt's local cookie store
- [ ] Cookies stored encrypted at rest using AES-256-GCM in ctxt's cookie database
- [ ] Cookie refresh happens automatically when extension detects session changes (re-login, token rotation)
- [ ] `ctxt cookie list` shows stored domains, cookie count, and expiry timestamps
- [ ] `ctxt cookie clear <domain>` removes all stored cookies for the specified domain
- [ ] `ctxt cookie clear --all` removes all stored cookies
- [ ] Cookies are domain-scoped and never sent to a different domain than their origin
- [ ] Extension has a "Capture this page" toolbar button for full-page capture
- [ ] Extension has a right-click context menu with options: "Capture full page", "Capture selection", "Capture element"
- [ ] Captured content is sent to `POST /api/v1/analyze` with cookie context for any linked resources
- [ ] Works with any website -- no platform-specific logic required for basic capture
- [ ] Extension communicates with ctxt daemon via localhost-only WebSocket (never over network)
- [ ] Extension shows capture status (pending, processing, completed) via badge/popup
- [ ] Cookie store supports configurable domain allowlist and blocklist

---

## Implementation Notes

### CLI Interface

```bash
# List all stored cookie domains
ctxt cookie list
DOMAIN                  COOKIES  EXPIRES              LAST REFRESHED
twitter.com             4        2026-03-15T08:00:00Z 2026-02-18T10:30:00Z
github.com              6        2026-03-20T12:00:00Z 2026-02-18T09:15:00Z
linkedin.com            3        2026-02-25T18:00:00Z 2026-02-17T14:22:00Z

# List cookies for a specific domain (verbose)
ctxt cookie list --domain github.com
DOMAIN       NAME              EXPIRES              SECURE  HTTPONLY
github.com   _gh_sess          2026-03-20T12:00:00Z true    true
github.com   user_session      2026-03-20T12:00:00Z true    true
github.com   logged_in         2026-03-20T12:00:00Z true    false
github.com   dotcom_user       2026-03-20T12:00:00Z true    false
github.com   _device_id        2026-06-18T09:15:00Z true    true
github.com   color_mode        2026-06-18T09:15:00Z false   false

# Clear cookies for a domain
ctxt cookie clear github.com
Cleared 6 cookies for github.com

# Clear all cookies
ctxt cookie clear --all
Cleared 13 cookies across 3 domains

# Check cookie health (test if sessions are still valid)
ctxt cookie check github.com
github.com: valid (authenticated as @jadb)

ctxt cookie check linkedin.com
linkedin.com: expired (session cookie _li_at expired 2026-02-16T00:00:00Z)

# Capture using stored cookies (manual URL capture with auth)
ctxt capture https://twitter.com/elonmusk/status/123456789
{
  "job_id": "j-cap-a1b2c3",
  "object_id": "o-cap-d4e5f6",
  "status": "pending_enrichment",
  "pipeline": "web.authenticated",
  "source": "https://twitter.com/elonmusk/status/123456789",
  "auth": "cookie_bridge:twitter.com"
}
```

### REST API

```
POST /api/v1/analyze
Content-Type: application/json

{
  "source_type": "url",
  "source_url": "https://twitter.com/elonmusk/status/123456789",
  "auth_method": "cookie_bridge",
  "capture_mode": "full_page"
}

-> 202 Accepted
{
  "job_id": "j-cap-a1b2c3",
  "object_id": "o-cap-d4e5f6",
  "status": "pending_enrichment",
  "pipeline": "web.authenticated"
}
```

Extension-initiated capture:

```
POST /api/v1/analyze
Content-Type: application/json

{
  "source_type": "browser_capture",
  "source_url": "https://example.com/article",
  "capture_mode": "selection",
  "content": "<selected HTML fragment>",
  "content_type": "text/html",
  "cookies_domain": "example.com"
}

-> 202 Accepted
{
  "job_id": "j-cap-g7h8i9",
  "object_id": "o-cap-j0k1l2",
  "status": "pending_enrichment",
  "pipeline": "web.authenticated"
}
```

Cookie management API (used by extension):

```
POST /api/v1/cookies
Content-Type: application/json

{
  "domain": "github.com",
  "cookies": [
    {
      "name": "_gh_sess",
      "value": "<encrypted_value>",
      "domain": ".github.com",
      "path": "/",
      "expires": "2026-03-20T12:00:00Z",
      "secure": true,
      "httpOnly": true
    }
  ]
}

-> 200 OK
{
  "domain": "github.com",
  "cookies_stored": 6,
  "expires_at": "2026-03-20T12:00:00Z"
}

GET /api/v1/cookies
-> 200 OK
{
  "domains": [
    {"domain": "github.com", "cookie_count": 6, "expires_at": "2026-03-20T12:00:00Z"},
    {"domain": "twitter.com", "cookie_count": 4, "expires_at": "2026-03-15T08:00:00Z"}
  ]
}

DELETE /api/v1/cookies/github.com
-> 204 No Content
```

### Pipeline Steps

**`web.authenticated`** (cookie-bridged web capture):

```
CookieInjector -> HTMLFetcher -> ReadabilityConverter -> Sectioner -> Tagger -> EmbeddingGenerator
```

Step responsibilities:

| Step | Input | Output |
|------|-------|--------|
| CookieInjector | URL + cookie store | Configured HTTP client with domain-scoped cookies injected |
| HTMLFetcher | Authenticated HTTP client + URL | Raw HTML response, HTTP headers, status code |
| ReadabilityConverter | Raw HTML | Cleaned article content (title, byline, body) via readability algorithm |
| Sectioner | Cleaned article content | Structured Sections preserving heading hierarchy |
| Tagger | Sections + full text | Tags from vocabulary |
| EmbeddingGenerator | Sections + full text | Embeddings per section + full-document embedding |

### Browser Extension Architecture

```
+------------------+     localhost:9377     +------------------+
|  Browser Ext.    | <===================> |  ctxt daemon     |
|  (content.js +   |     WebSocket         |  (cookie store + |
|   background.js) |                       |   capture API)   |
+------------------+                       +------------------+
       |                                          |
       | observes cookie                          | encrypts + stores
       | changes via                              | in cookies.db
       | chrome.cookies API                       |
       |                                          |
       v                                          v
  [Browser Session]                        [AES-256-GCM
   cookies for                              encrypted SQLite]
   configured domains
```

Extension components:

```javascript
// background.js -- listens for cookie changes on configured domains
chrome.cookies.onChanged.addListener((changeInfo) => {
  const { cookie, removed } = changeInfo;
  const domain = cookie.domain.replace(/^\./, '');

  if (isConfiguredDomain(domain) && !removed) {
    syncCookiesToDaemon(domain);
  }
});

async function syncCookiesToDaemon(domain) {
  const cookies = await chrome.cookies.getAll({ domain });
  const ws = getWebSocketConnection();
  ws.send(JSON.stringify({
    type: 'cookie_sync',
    domain,
    cookies: cookies.map(c => ({
      name: c.name,
      value: c.value,
      domain: c.domain,
      path: c.path,
      expires: c.expirationDate,
      secure: c.secure,
      httpOnly: c.httpOnly,
    })),
  }));
}

// content.js -- capture selected content
function captureSelection() {
  const selection = window.getSelection();
  const range = selection.getRangeAt(0);
  const container = document.createElement('div');
  container.appendChild(range.cloneContents());

  chrome.runtime.sendMessage({
    type: 'capture',
    mode: 'selection',
    url: window.location.href,
    content: container.innerHTML,
    title: document.title,
  });
}
```

### Backend Processing

1. Browser extension detects user is logged into a configured domain
2. Extension reads cookies via `chrome.cookies.getAll()` for that domain
3. Extension sends cookies to ctxt daemon over localhost WebSocket (port 9377)
4. Daemon encrypts cookies with AES-256-GCM using a key derived from the user's ctxt master key
5. Encrypted cookies stored in `~/.ctxt/cookies.db` (SQLite), indexed by domain
6. When user triggers capture (extension button, context menu, or CLI `ctxt capture <url>`):
   a. CookieInjector step looks up the URL's domain in the cookie store
   b. If cookies found, creates an authenticated HTTP client with those cookies injected
   c. If no cookies found, falls back to unauthenticated fetch (may get limited content)
7. HTMLFetcher uses the authenticated client to fetch the page
8. ReadabilityConverter extracts the main article content, stripping navigation, ads, sidebars
9. Sectioner creates structured Sections from the article's heading hierarchy
10. Tagger and EmbeddingGenerator complete the enrichment
11. KnowledgeObject persisted with source URL and `auth_method: cookie_bridge` in metadata

### Cookie Store Schema

```sql
CREATE TABLE cookies (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    domain      TEXT NOT NULL,
    name        TEXT NOT NULL,
    value_enc   BLOB NOT NULL,         -- AES-256-GCM encrypted value
    iv          BLOB NOT NULL,         -- Initialization vector
    path        TEXT NOT NULL DEFAULT '/',
    expires_at  DATETIME,
    secure      BOOLEAN NOT NULL DEFAULT 1,
    http_only   BOOLEAN NOT NULL DEFAULT 1,
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(domain, name, path)
);

CREATE INDEX idx_cookies_domain ON cookies(domain);
CREATE INDEX idx_cookies_expires ON cookies(expires_at);
```

### Configuration

```yaml
# In configuration.yaml
cookies:
  # Path to encrypted cookie store
  storePath: ~/.ctxt/cookies.db

  # Encryption algorithm for cookie values at rest
  encryption: aes-256-gcm

  # Master key derivation (from ctxt user key)
  keyDerivation: argon2id

  # Domain allowlist ("*" = all domains, or explicit list)
  allowedDomains:
    - "*"

  # Domain blocklist (overrides allowlist)
  blockedDomains:
    - "*.bank.com"
    - "*.gov"

  # Maximum cookie age before forced expiry
  maxAge: 30d

  # Re-sync cookies when they are accessed for a fetch
  refreshOnAccess: true

  # Auto-purge expired cookies
  autoPurge: true
  purgeInterval: 1h

  # Daemon WebSocket settings
  daemon:
    host: 127.0.0.1
    port: 9377
    # TLS for localhost (optional, recommended)
    tls:
      enabled: false
      certFile: ~/.ctxt/daemon.crt
      keyFile: ~/.ctxt/daemon.key

# Extension capture settings
capture:
  # Default capture mode: full_page | selection | element
  defaultMode: full_page

  # Keyboard shortcut for capture (configured in extension)
  shortcut: "Ctrl+Shift+C"

  # Auto-detect and select platform-specific pipeline
  autoDetectPipeline: true
```

### KnowledgeObject Structure

```json
{
  "id": "o-cap-d4e5f6",
  "type": "web",
  "subtype": "authenticated",
  "raw_content": "<full article text>",
  "content_type": "text/html",
  "source": {
    "type": "url",
    "url": "https://example.com/premium-article",
    "captured_at": "2026-02-18T10:30:45Z",
    "auth_method": "cookie_bridge",
    "auth_domain": "example.com"
  },
  "metadata": {
    "title": "Understanding Distributed Systems",
    "byline": "Jane Smith",
    "site_name": "Example Blog",
    "word_count": 2847,
    "capture_mode": "full_page",
    "http_status": 200
  },
  "sections": [
    {
      "id": "sec-001",
      "title": "Introduction",
      "content": "Distributed systems are fundamentally about..."
    },
    {
      "id": "sec-002",
      "title": "Consensus Protocols",
      "content": "The most well-known consensus protocol is Raft..."
    }
  ],
  "tags": ["distributed-systems", "consensus", "raft"],
  "mentions": [{"entity": "@raft"}, {"entity": "@person.jane-smith"}],
  "pipeline": {
    "name": "web.authenticated",
    "steps_completed": ["cookie_injector", "html_fetcher", "readability_converter", "sectioner", "tagger", "embedding_generator"],
    "completed_at": "2026-02-18T10:31:02Z"
  }
}
```

---

## E2E Test Checklist

- [ ] Extension: Chrome extension installs and registers with ctxt daemon via WebSocket
- [ ] Extension: Firefox extension installs and registers with ctxt daemon via WebSocket
- [ ] Extension: "Capture this page" button sends full page HTML to ctxt daemon
- [ ] Extension: Right-click context menu appears with capture options (full page, selection, element)
- [ ] Extension: Selected text capture sends only the selected HTML fragment
- [ ] Extension: DOM element capture sends the targeted element's outer HTML
- [ ] Cookies: Extension detects login on configured domain and syncs cookies to daemon
- [ ] Cookies: `ctxt cookie list` shows stored domains with correct expiry timestamps
- [ ] Cookies: `ctxt cookie list --domain github.com` flag is sent to the server and returns only github.com cookies (not all domains)
- [ ] Cookies: `ctxt cookie clear github.com` removes only github.com cookies
- [ ] Cookies: `ctxt cookie clear --all` flag is present in the request and removes all cookies across all domains
- [ ] Cookies: `ctxt cookie check <domain>` correctly reports valid/expired sessions
- [ ] Security: Cookies stored encrypted at rest (raw values not readable in cookies.db)
- [ ] Security: Domain scoping enforced -- github.com cookies never sent to twitter.com
- [ ] Security: Blocked domains (e.g., *.bank.com) rejected even if extension tries to sync
- [ ] Security: WebSocket only binds to 127.0.0.1, not accessible from network
- [ ] Fetch: Authenticated page fetch succeeds with valid cookies (returns full content)
- [ ] Fetch: Unauthenticated fallback works when no cookies stored for domain
- [ ] Fetch: Expired cookies trigger re-sync notification to extension
- [ ] Pipeline: `web.authenticated` pipeline completes all steps successfully
- [ ] Pipeline: ReadabilityConverter extracts article content, strips navigation/ads
- [ ] REST API: `POST /api/v1/analyze` request payload contains `source_type: url`, `auth_method: cookie_bridge`, and `capture_mode: full_page`
- [ ] REST API: `POST /api/v1/analyze` with `source_type: browser_capture` request payload contains `source_url`, `capture_mode`, `content`, and `cookies_domain` fields
- [ ] REST API: `POST /api/v1/analyze` returns 202 with `job_id` and `object_id`
- [ ] REST API: `POST /api/v1/cookies` request payload contains `domain` and `cookies[]` array with `name`, `value`, `domain`, `expires`, `secure`, `httpOnly` fields; response confirms `cookies_stored` count
- [ ] REST API: `POST /api/v1/cookies` stored cookies are retrievable via `GET /api/v1/cookies` and appear in the response `domains` list
- [ ] REST API: `DELETE /api/v1/cookies/<domain>` removes cookies and subsequent `GET /api/v1/cookies` no longer lists the domain
- [ ] Storage: Captured object stored with `source.auth_method: cookie_bridge` and `source.auth_domain` in metadata (verifiable via `GET /api/v1/objects/<id>`)
- [ ] Resilience: Daemon restart preserves cookie store (persistent SQLite)
- [ ] Resilience: Extension reconnects to daemon after WebSocket disconnect

---

## Related Stories

- [US-0002](../ingestion/US-0002-url-capture-and-extraction.md) -- Base URL capture; cookie bridge extends this with authentication
- [US-0201](./US-0201-x-twitter-capture.md) -- X/Twitter capture depends on cookie bridge for authenticated access
- [US-0202](./US-0202-github-capture.md) -- GitHub capture can use cookie bridge as alternative to PAT
- [US-0204](./US-0204-linkedin-capture.md) -- LinkedIn capture requires cookie bridge (no public API)
- [US-0001](../ingestion/US-0001-text-capture-minimal-friction.md) -- Base capture flow and async job pattern
- [US-0031](../admin/US-0031-configure-encryption-and-secrets.md) -- Encryption configuration shared with cookie store

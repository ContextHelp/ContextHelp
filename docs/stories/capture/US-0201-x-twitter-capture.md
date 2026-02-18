# US-0201: X/Twitter Profile and Thread Capture

**System Types:** ctxt
**Personas:** [Knowledge Workers](../../personas/knowledge-workers.md), [Researchers & OSINT Analysts](../../personas/knowledge-workers.md)

---

## User Goal

As a researcher, I want to capture X/Twitter profiles, individual posts, and full threads into my knowledge base so that social media content becomes searchable and cross-referenceable.

---

## Context

X/Twitter has become a primary channel for real-time discourse in technology, science, policy, and business. Researchers tracking emerging trends, journalists following sources, and knowledge workers curating domain expertise all encounter high-value content in tweets and threads that disappears from memory within hours. Threads in particular -- where an author develops an argument across 10-20 connected posts -- represent structured thinking that rivals blog posts in depth but lacks their permanence and searchability.

The challenge is that X/Twitter's content is increasingly gated. Anonymous access sees a fraction of the content: replies are hidden, quote tweets are collapsed, and profiles may be restricted. Authenticated access (via the user's browser session cookies, shared through the cookie bridge in US-0200) unlocks the full view. The system must gracefully handle both modes: capture what is available anonymously but indicate when authenticated access would yield richer results.

Beyond raw text, X/Twitter posts carry structured metadata that enriches the knowledge graph. Each post has engagement counts (likes, retweets, replies) that signal content importance. Quoted posts create citation-like relationships. User profiles contain bios, follower counts, and pinned posts that contextualize the author. Thread detection (following reply chains to reconstruct the full narrative) transforms a series of disconnected posts into a coherent document. All of this structured data feeds into entity resolution: the X user `@elonmusk` can be linked to a canonical entity representing the person across platforms.

---

## Acceptance Criteria

- [ ] `ctxt capture https://x.com/username` captures profile: bio, location, pinned post, recent posts (up to 20)
- [ ] `ctxt capture https://x.com/username/status/123` captures a single post with its direct replies
- [ ] Thread detection: when a post is part of a thread, the full thread is automatically captured
- [ ] `ctxt capture https://x.com/username/status/123 --no-thread` captures only the single post
- [ ] Uses stored browser cookies for authenticated access when available (sees more content)
- [ ] Falls back to unauthenticated access when no cookies stored (captures what is publicly visible)
- [ ] Extracts: post text, media links (images, videos), engagement counts, timestamps, quoted posts
- [ ] Creates an entity for the X user (`@x.username`) linked to canonical entity if known
- [ ] Quoted posts are captured as separate linked KnowledgeObjects with `quotes` edges
- [ ] Handles rate limiting gracefully: exponential backoff, retry, and user notification
- [ ] Stores raw HTML snapshot as provenance alongside extracted structured data
- [ ] Media attachments (images) are downloaded and processed through the image pipeline
- [ ] Processing is async -- returns job ID immediately

---

## Implementation Notes

### CLI Interface

```bash
# Capture a user profile (bio, pinned post, recent posts)
ctxt capture https://x.com/karpathy
{
  "job_id": "j-x-prof-1a2b3c",
  "object_id": "o-x-prof-4d5e6f",
  "status": "pending_enrichment",
  "pipeline": "social.x.profile",
  "source": "https://x.com/karpathy",
  "auth": "cookie_bridge:x.com"
}

# Capture a single post (auto-detects thread)
ctxt capture https://x.com/karpathy/status/1234567890
{
  "job_id": "j-x-post-7g8h9i",
  "object_id": "o-x-post-0j1k2l",
  "status": "pending_enrichment",
  "pipeline": "social.x.post",
  "source": "https://x.com/karpathy/status/1234567890",
  "metadata": {
    "thread_detected": true,
    "thread_length": 12,
    "has_quoted_posts": true
  }
}

# Capture single post only, skip thread detection
ctxt capture https://x.com/karpathy/status/1234567890 --no-thread

# Capture with explicit pipeline
ctxt capture https://x.com/karpathy --pipeline social.x.profile

# Check job progress
ctxt job get j-x-post-7g8h9i
{
  "job_id": "j-x-post-7g8h9i",
  "status": "processing",
  "pipeline": "social.x.post",
  "progress": {
    "current_step": "ThreadDetector",
    "steps_completed": 2,
    "steps_total": 7,
    "percent": 28
  }
}
```

### REST API

```
POST /api/v1/analyze
Content-Type: application/json

{
  "source_type": "url",
  "source_url": "https://x.com/karpathy/status/1234567890",
  "auth_method": "cookie_bridge",
  "options": {
    "detect_thread": true,
    "capture_media": true,
    "capture_quoted": true
  }
}

-> 202 Accepted
{
  "job_id": "j-x-post-7g8h9i",
  "object_id": "o-x-post-0j1k2l",
  "status": "pending_enrichment",
  "pipeline": "social.x.post"
}
```

Retrieving the captured thread:

```
GET /api/v1/objects/o-x-post-0j1k2l

-> 200 OK
{
  "id": "o-x-post-0j1k2l",
  "type": "social",
  "subtype": "x.thread",
  "raw_content": "1/12 Let me explain how neural networks actually learn...",
  "source": {
    "type": "url",
    "url": "https://x.com/karpathy/status/1234567890",
    "captured_at": "2026-02-18T10:30:45Z",
    "auth_method": "cookie_bridge"
  },
  "metadata": {
    "platform": "x.com",
    "author_handle": "@karpathy",
    "author_name": "Andrej Karpathy",
    "thread_length": 12,
    "total_likes": 45230,
    "total_retweets": 8912,
    "total_replies": 1247,
    "posted_at": "2026-02-17T14:22:00Z",
    "has_media": true,
    "media_count": 3
  },
  "sections": [
    {
      "id": "sec-tweet-001",
      "title": "Thread 1/12",
      "content": "Let me explain how neural networks actually learn...",
      "metadata": {
        "tweet_id": "1234567890",
        "likes": 12340,
        "retweets": 3210,
        "replies": 456,
        "posted_at": "2026-02-17T14:22:00Z"
      }
    },
    {
      "id": "sec-tweet-002",
      "title": "Thread 2/12",
      "content": "First, you need to understand gradient descent...",
      "metadata": {
        "tweet_id": "1234567891",
        "likes": 8920,
        "retweets": 2100,
        "replies": 234,
        "posted_at": "2026-02-17T14:23:15Z",
        "media": [{"type": "image", "url": "https://pbs.twimg.com/media/..."}]
      }
    }
  ],
  "children": [
    {
      "object_id": "o-x-quote-m3n4o5",
      "type": "social",
      "relationship": "quotes",
      "metadata": {"quoted_in_tweet": "1234567895"}
    },
    {
      "object_id": "o-img-p6q7r8",
      "type": "image",
      "relationship": "media_attachment",
      "metadata": {"from_tweet": "1234567891"}
    }
  ],
  "tags": ["machine-learning", "neural-networks", "gradient-descent"],
  "mentions": [{"entity": "@x.karpathy"}, {"entity": "@concept.neural-networks"}]
}
```

### Pipeline Steps

**`social.x.profile`** (profile capture):

```
CookieFetcher -> XProfileParser -> EntityResolver -> Sectioner -> Tagger -> EmbeddingGenerator
```

**`social.x.post`** (post and thread capture):

```
CookieFetcher -> XPostParser -> ThreadDetector -> EntityResolver -> Sectioner -> Tagger -> EmbeddingGenerator
```

Step responsibilities:

| Step | Input | Output |
|------|-------|--------|
| CookieFetcher | URL + cookie store | Authenticated HTTP response (raw HTML + JSON from embedded data) |
| XProfileParser | Profile HTML/JSON | Structured profile data: bio, stats, pinned post, recent posts |
| XPostParser | Post HTML/JSON | Structured post data: text, media, engagement, quoted posts |
| ThreadDetector | Single post data | Full thread (follows reply chain up and down), sets `thread_length` |
| EntityResolver | Structured data | Creates/links `@x.username` entity, resolves to canonical if known |
| Sectioner | Thread/profile data | Sections: one per tweet in thread, or profile sections (bio, pinned, posts) |
| Tagger | Sections + full text | Tags from vocabulary |
| EmbeddingGenerator | Sections + full text | Embeddings per section + full-document embedding |

### Backend Processing

1. User provides X/Twitter URL via CLI (`ctxt capture <url>`) or REST API
2. URL pattern determines pipeline: `/username` -> `social.x.profile`, `/username/status/ID` -> `social.x.post`
3. CookieFetcher retrieves cookies for `x.com` from the cookie store; constructs authenticated HTTP client
4. CookieFetcher fetches the page HTML; extracts embedded JSON data (X embeds structured data in `<script>` tags)
5. XPostParser extracts: post text, author handle, timestamp, engagement counts, media URLs, quoted post references
6. ThreadDetector (if enabled) follows the reply chain:
   a. Checks if post is a reply to the same author (self-reply = thread continuation)
   b. Follows reply chain upward to find thread root
   c. Follows reply chain downward to find all thread posts
   d. Assembles ordered thread with position markers (1/N, 2/N, ...)
7. For each media attachment (image), creates a child KnowledgeObject and queues it for the image pipeline
8. For each quoted post, creates a separate KnowledgeObject linked with a `quotes` edge
9. EntityResolver creates an entity for the X user (`@x.username`) with profile metadata
10. If the user is already known (e.g., captured their profile previously), links to the canonical entity
11. Sectioner creates one Section per tweet in the thread, preserving order and engagement metadata
12. Tagger assigns tags based on tweet content (hashtags become tags, topic detection on full thread text)
13. EmbeddingGenerator creates per-tweet embeddings plus a full-thread embedding
14. Raw HTML snapshot stored alongside structured data for provenance
15. Job status updated to `completed`

### X/Twitter URL Pattern Detection

```go
type XURLParser struct{}

func (p *XURLParser) Parse(rawURL string) (*XCapture, error) {
    u, err := url.Parse(rawURL)
    if err != nil {
        return nil, err
    }

    host := strings.TrimPrefix(u.Hostname(), "www.")
    if host != "x.com" && host != "twitter.com" {
        return nil, fmt.Errorf("not an X/Twitter URL: %s", host)
    }

    parts := strings.Split(strings.Trim(u.Path, "/"), "/")

    switch {
    case len(parts) == 1:
        // https://x.com/username -> profile
        return &XCapture{Type: "profile", Username: parts[0]}, nil
    case len(parts) == 3 && parts[1] == "status":
        // https://x.com/username/status/123 -> post
        return &XCapture{Type: "post", Username: parts[0], PostID: parts[2]}, nil
    default:
        return nil, fmt.Errorf("unrecognized X URL pattern: %s", u.Path)
    }
}
```

### Configuration

```yaml
# In configuration.yaml
social:
  x:
    # Thread detection: follow self-reply chains
    detectThreads: true

    # Maximum thread length to capture (prevent runaway threads)
    maxThreadLength: 100

    # Capture quoted posts as linked objects
    captureQuotedPosts: true

    # Download and process media attachments
    captureMedia: true

    # Rate limiting
    rateLimit:
      requestsPerMinute: 30
      backoffMultiplier: 2.0
      maxRetries: 5
      maxBackoffSeconds: 300

    # How many recent posts to capture for profile
    profileRecentPosts: 20

    # Store raw HTML snapshot for provenance
    storeRawHTML: true

    # Accept both x.com and twitter.com URLs
    domains:
      - x.com
      - twitter.com
```

### KnowledgeObject Structure (Profile)

```json
{
  "id": "o-x-prof-4d5e6f",
  "type": "social",
  "subtype": "x.profile",
  "raw_content": "Andrej Karpathy. Building AI. Previously Director of AI at Tesla...",
  "source": {
    "type": "url",
    "url": "https://x.com/karpathy",
    "captured_at": "2026-02-18T10:30:45Z"
  },
  "metadata": {
    "platform": "x.com",
    "handle": "@karpathy",
    "display_name": "Andrej Karpathy",
    "bio": "Building AI. Previously Director of AI at Tesla, OpenAI.",
    "location": "San Francisco",
    "joined": "2012-06-01",
    "followers": 920000,
    "following": 450,
    "post_count": 8200,
    "verified": true
  },
  "sections": [
    {"id": "sec-bio", "title": "Bio", "content": "Building AI. Previously Director of AI at Tesla, OpenAI."},
    {"id": "sec-pinned", "title": "Pinned Post", "content": "I just released a new video on building GPT from scratch..."},
    {"id": "sec-recent-01", "title": "Recent Post (2026-02-17)", "content": "Interesting paper on..."}
  ],
  "tags": ["ai", "machine-learning", "tesla", "openai"],
  "mentions": [{"entity": "@x.karpathy"}, {"entity": "@org.tesla"}, {"entity": "@org.openai"}]
}
```

---

## E2E Test Checklist

- [ ] CLI: `ctxt capture https://x.com/username` returns job ID within 2 seconds
- [ ] CLI: `ctxt capture https://x.com/username/status/123` returns job ID within 2 seconds
- [ ] Profile: Bio, location, follower count, and recent posts captured correctly
- [ ] Profile: Pinned post included in captured profile
- [ ] Post: Single post text, timestamp, and engagement counts captured
- [ ] Post: Media attachments (images) downloaded and linked as child objects
- [ ] Thread: Self-reply chain detected and full thread captured in order
- [ ] Thread: Thread position markers (1/N) applied to Sections
- [ ] Thread: `--no-thread` flag captures only the target post
- [ ] Quoted: Quoted posts captured as separate linked KnowledgeObjects
- [ ] Entity: X user entity created with handle, display name, and profile URL
- [ ] Entity: Same user captured twice resolves to the same entity (idempotent)
- [ ] Auth: Authenticated capture (with cookies) returns full content
- [ ] Auth: Unauthenticated capture (no cookies) returns publicly visible content
- [ ] Auth: Missing cookies triggers informational message suggesting cookie bridge setup
- [ ] Rate Limit: Rapid successive captures trigger backoff without crashing
- [ ] Rate Limit: Rate-limited response (HTTP 429) causes exponential backoff and retry
- [ ] Provenance: Raw HTML snapshot stored alongside structured data
- [ ] Search: Thread content searchable via `ctxt search "gradient descent"`
- [ ] Search: Profile bio searchable via `ctxt search "Director of AI"`
- [ ] REST API: POST /api/v1/analyze with X URL returns 202
- [ ] Error: Invalid X URL (e.g., https://x.com/) returns descriptive error
- [ ] Error: Deleted post returns clear "content not found" error
- [ ] Pipeline: `social.x.post` completes all 7 steps in correct order

---

## Related Stories

- [US-0200](./US-0200-browser-cookie-bridge.md) -- Cookie bridge provides authenticated access to X/Twitter
- [US-0002](../ingestion/US-0002-url-capture-and-extraction.md) -- Base URL capture; X capture extends with platform-specific parsing
- [US-0003](../ingestion/US-0003-image-ocr-and-analysis.md) -- Media attachments processed through image pipeline
- [US-0009](../enrichment/US-0009-extract-entities-and-mentions.md) -- Entity extraction from tweet content
- [US-0204](./US-0204-linkedin-capture.md) -- Similar social platform capture pattern for LinkedIn
- [US-0205](./US-0205-wikipedia-capture.md) -- Reference capture for linking mentioned concepts

# US-0204: LinkedIn Profile and Post Capture

**System Types:** ctxt
**Personas:** [Knowledge Workers](../../personas/knowledge-workers.md), [Researchers & OSINT Analysts](../../personas/knowledge-workers.md)

---

## User Goal

As a recruiter/researcher, I want to capture LinkedIn profiles and posts into my knowledge base using my browser session so that professional network information becomes searchable and cross-referenceable.

---

## Context

LinkedIn is the primary repository of professional identity on the internet. For recruiters evaluating candidates, researchers mapping industry expertise, business developers identifying partners, and knowledge workers tracking their professional network, LinkedIn profiles contain structured data that is unavailable anywhere else: career timelines, skill endorsements, educational backgrounds, recommendation letters, and professional publications. Yet this information is locked inside LinkedIn's walled garden, unsearchable outside the platform and impossible to cross-reference with other knowledge sources.

The challenge with LinkedIn is that it has no practical public API for profile data extraction. The official API is restricted to first-party applications with partnership agreements, and even then provides limited data. This makes the browser cookie bridge (US-0200) not just useful but essential -- it is the only viable path to capturing LinkedIn content. The user's authenticated browser session, shared through the extension, allows ctxt to see exactly what the user sees when visiting a profile.

LinkedIn also employs aggressive anti-scraping measures: rate limiting, bot detection, CAPTCHA challenges, and dynamic page rendering that defeats simple HTML parsing. The capture pipeline must handle these gracefully. When LinkedIn detects unusual activity, the system should pause, notify the user, and wait rather than hammering the endpoint and risking account restrictions. Careful rate limiting (configurable delays between requests) and human-like access patterns (random delays, session-realistic headers) are essential. The structured data extracted from profiles -- job titles, companies, skills, education -- creates rich entities that connect to the broader knowledge graph, enabling queries like "who in my network has experience with distributed systems at companies over 1000 employees."

---

## Acceptance Criteria

- [ ] `ctxt capture https://linkedin.com/in/username` captures profile: headline, summary, experience, education, skills, recommendations
- [ ] `ctxt capture https://linkedin.com/posts/username_activity-123` captures post with comments
- [ ] Uses browser cookies exclusively (LinkedIn has no practical public API for this use case)
- [ ] Structures experience entries as Sections with company entities
- [ ] Education entries structured as Sections with institution entities
- [ ] Skills extracted and mapped to tags
- [ ] Creates entity for the LinkedIn user (`@linkedin.username`) linked to canonical entity if known
- [ ] Creates entities for companies and institutions mentioned in experience/education
- [ ] Handles LinkedIn anti-scraping gracefully: rate limits, delays, CAPTCHA detection with pause-and-notify
- [ ] Stores raw HTML snapshot as provenance alongside extracted structured data
- [ ] Recommendations captured and attributed to recommender entities
- [ ] Processing is async -- returns job ID immediately
- [ ] Clear error message when no LinkedIn cookies are available in cookie store
- [ ] Captures profile and post content visible to the authenticated user (respects LinkedIn's access model)

---

## Implementation Notes

### CLI Interface

```bash
# Capture a LinkedIn profile
ctxt capture https://linkedin.com/in/satyanadella
{
  "job_id": "j-li-prof-1a2b3c",
  "object_id": "o-li-prof-4d5e6f",
  "status": "pending_enrichment",
  "pipeline": "social.linkedin.profile",
  "source": "https://linkedin.com/in/satyanadella",
  "auth": "cookie_bridge:linkedin.com"
}

# Capture a LinkedIn post
ctxt capture https://linkedin.com/posts/satyanadella_activity-7164534289012850688
{
  "job_id": "j-li-post-7g8h9i",
  "object_id": "o-li-post-0j1k2l",
  "status": "pending_enrichment",
  "pipeline": "social.linkedin.post",
  "source": "https://linkedin.com/posts/satyanadella_activity-7164534289012850688"
}

# Capture with www prefix (normalized)
ctxt capture https://www.linkedin.com/in/satyanadella

# Check job progress
ctxt job get j-li-prof-1a2b3c
{
  "job_id": "j-li-prof-1a2b3c",
  "status": "processing",
  "pipeline": "social.linkedin.profile",
  "progress": {
    "current_step": "ExperienceDecomposer",
    "steps_completed": 2,
    "steps_total": 7,
    "percent": 28
  }
}

# Verify LinkedIn cookies are available
ctxt cookie check linkedin.com
linkedin.com: valid (authenticated session detected)

# Error when no cookies available
ctxt capture https://linkedin.com/in/someone
Error: No cookies stored for linkedin.com. LinkedIn requires authenticated
access via the browser cookie bridge. Install the ctxt browser extension
and log into LinkedIn to enable capture.
See: https://docs.ctxt.dev/capture/cookie-bridge
```

### REST API

```
POST /api/v1/analyze
Content-Type: application/json

{
  "source_type": "url",
  "source_url": "https://linkedin.com/in/satyanadella",
  "auth_method": "cookie_bridge",
  "options": {
    "capture_experience": true,
    "capture_education": true,
    "capture_skills": true,
    "capture_recommendations": true
  }
}

-> 202 Accepted
{
  "job_id": "j-li-prof-1a2b3c",
  "object_id": "o-li-prof-4d5e6f",
  "status": "pending_enrichment",
  "pipeline": "social.linkedin.profile"
}
```

Retrieving the captured profile:

```
GET /api/v1/objects/o-li-prof-4d5e6f

-> 200 OK
{
  "id": "o-li-prof-4d5e6f",
  "type": "social",
  "subtype": "linkedin.profile",
  "raw_content": "Satya Nadella. Chairman and CEO at Microsoft...",
  "source": {
    "type": "url",
    "url": "https://linkedin.com/in/satyanadella",
    "captured_at": "2026-02-18T10:30:45Z",
    "auth_method": "cookie_bridge"
  },
  "metadata": {
    "platform": "linkedin.com",
    "username": "satyanadella",
    "full_name": "Satya Nadella",
    "headline": "Chairman and CEO at Microsoft",
    "location": "Redmond, Washington, United States",
    "connections": "500+",
    "followers": 10500000,
    "profile_url": "https://linkedin.com/in/satyanadella",
    "experience_count": 4,
    "education_count": 3,
    "skills_count": 15,
    "recommendations_count": 8
  },
  "sections": [
    {
      "id": "sec-headline",
      "title": "Headline",
      "content": "Chairman and CEO at Microsoft"
    },
    {
      "id": "sec-about",
      "title": "About",
      "content": "I am passionate about using technology to help people and organizations around the world achieve more..."
    },
    {
      "id": "sec-exp-001",
      "title": "Experience: Chairman and CEO at Microsoft",
      "content": "Feb 2014 - Present. Leading Microsoft's mission to empower every person and every organization on the planet to achieve more.",
      "metadata": {
        "section_type": "experience",
        "company": "Microsoft",
        "title": "Chairman and CEO",
        "start_date": "2014-02",
        "end_date": null,
        "current": true,
        "duration": "12 years",
        "location": "Redmond, Washington"
      }
    },
    {
      "id": "sec-exp-002",
      "title": "Experience: EVP, Cloud and Enterprise at Microsoft",
      "content": "2011 - 2014. Led the transformation of Microsoft's cloud infrastructure and developer platform.",
      "metadata": {
        "section_type": "experience",
        "company": "Microsoft",
        "title": "Executive Vice President, Cloud and Enterprise",
        "start_date": "2011",
        "end_date": "2014-02",
        "current": false,
        "duration": "3 years"
      }
    },
    {
      "id": "sec-edu-001",
      "title": "Education: University of Wisconsin-Milwaukee",
      "content": "MS, Computer Science. 1990.",
      "metadata": {
        "section_type": "education",
        "institution": "University of Wisconsin-Milwaukee",
        "degree": "MS",
        "field": "Computer Science",
        "year": 1990
      }
    },
    {
      "id": "sec-edu-002",
      "title": "Education: University of Chicago Booth School of Business",
      "content": "MBA. 1997.",
      "metadata": {
        "section_type": "education",
        "institution": "University of Chicago Booth School of Business",
        "degree": "MBA",
        "field": "Business Administration",
        "year": 1997
      }
    },
    {
      "id": "sec-skills",
      "title": "Skills",
      "content": "Cloud Computing, Artificial Intelligence, Leadership, Strategic Planning, Software Engineering, Product Management, Enterprise Software, SaaS, Digital Transformation, Machine Learning, Distributed Systems, Innovation, Technology Strategy, Business Development, Public Speaking"
    },
    {
      "id": "sec-rec-001",
      "title": "Recommendation from Bill Gates",
      "content": "Satya has transformed Microsoft's culture and strategy...",
      "metadata": {
        "section_type": "recommendation",
        "recommender": "Bill Gates",
        "recommender_title": "Co-chair, Bill & Melinda Gates Foundation",
        "relationship": "Worked together at Microsoft"
      }
    }
  ],
  "tags": ["cloud-computing", "artificial-intelligence", "leadership", "microsoft", "ceo"],
  "mentions": [
    {"entity": "@linkedin.satyanadella"},
    {"entity": "@org.microsoft"},
    {"entity": "@edu.university-of-wisconsin-milwaukee"},
    {"entity": "@edu.university-of-chicago"},
    {"entity": "@person.bill-gates"}
  ]
}
```

### Pipeline Steps

**`social.linkedin.profile`** (profile capture):

```
CookieFetcher -> LinkedInProfileParser -> ExperienceDecomposer -> EntityResolver -> Sectioner -> Tagger -> EmbeddingGenerator
```

**`social.linkedin.post`** (post capture):

```
CookieFetcher -> LinkedInPostParser -> EntityResolver -> Sectioner -> Tagger -> EmbeddingGenerator
```

Step responsibilities:

| Step | Input | Output |
|------|-------|--------|
| CookieFetcher | URL + cookie store | Authenticated HTTP response (rendered LinkedIn page HTML) |
| LinkedInProfileParser | Profile HTML | Structured profile: name, headline, about, experience, education, skills, recommendations |
| LinkedInPostParser | Post HTML | Structured post: text, author, engagement, comments |
| ExperienceDecomposer | Structured experience entries | Individual experience/education Sections with company/institution metadata |
| EntityResolver | Structured data | Creates/links entities for person, companies, institutions |
| Sectioner | Decomposed profile data | Ordered Sections: headline, about, experience entries, education entries, skills, recommendations |
| Tagger | Sections + skills | Tags from vocabulary + skills mapped to tags |
| EmbeddingGenerator | Sections + full text | Embeddings per section + full-profile embedding |

### LinkedIn HTML Parsing

```go
type LinkedInProfileParser struct {
    selectors LinkedInSelectors
}

// LinkedIn uses dynamic rendering; we extract from the server-rendered HTML
// that the authenticated session receives. The selectors are version-aware
// and updated when LinkedIn changes their markup.
type LinkedInSelectors struct {
    Name            string // CSS selector for full name
    Headline        string // CSS selector for headline
    About           string // CSS selector for about/summary section
    ExperienceList  string // CSS selector for experience entries
    EducationList   string // CSS selector for education entries
    SkillsList      string // CSS selector for skills
    Recommendations string // CSS selector for recommendations
}

func (p *LinkedInProfileParser) Run(ctx context.Context, draft *KnowledgeObject) (*KnowledgeObject, error) {
    doc, err := goquery.NewDocumentFromReader(
        strings.NewReader(draft.RawContent.(string)),
    )
    if err != nil {
        return nil, fmt.Errorf("linkedin_profile_parser: %w", err)
    }

    // Check for anti-scraping detection
    if isAuthWall(doc) {
        return nil, fmt.Errorf("linkedin_profile_parser: authentication wall detected, cookies may be expired")
    }
    if isCaptchaPage(doc) {
        return nil, &CaptchaError{
            Message: "LinkedIn CAPTCHA detected. Please solve the CAPTCHA in your browser and retry.",
            Domain:  "linkedin.com",
        }
    }

    profile := extractProfile(doc, p.selectors)
    draft.Metadata["full_name"] = profile.Name
    draft.Metadata["headline"] = profile.Headline
    draft.Metadata["location"] = profile.Location
    // ... additional metadata

    return draft, nil
}
```

### Anti-Scraping Handling

```go
type AntiScrapingHandler struct {
    rateLimiter   *rate.Limiter
    randomDelay   time.Duration  // Random additional delay (0 to this value)
    captchaNotify func(domain string, message string)
}

func (h *AntiScrapingHandler) HandleResponse(resp *http.Response, domain string) error {
    switch {
    case resp.StatusCode == 429:
        // Rate limited -- back off significantly
        retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"))
        if retryAfter == 0 {
            retryAfter = 60 * time.Second
        }
        return &RateLimitError{
            RetryAfter: retryAfter,
            Message:    fmt.Sprintf("LinkedIn rate limit hit, retry after %s", retryAfter),
        }

    case resp.StatusCode == 999:
        // LinkedIn-specific: "Request denied" status code for suspected bots
        h.captchaNotify(domain, "LinkedIn detected automated access. Please visit linkedin.com in your browser to verify your session.")
        return &BotDetectionError{
            Domain:  domain,
            Message: "LinkedIn bot detection triggered (HTTP 999)",
        }

    case resp.StatusCode == 302 && isLoginRedirect(resp):
        return fmt.Errorf("LinkedIn session expired. Please log in via browser to refresh cookies.")
    }

    return nil
}
```

### Backend Processing

1. User provides LinkedIn URL via CLI (`ctxt capture <url>`) or REST API
2. URL pattern determines pipeline: `/in/username` -> `social.linkedin.profile`, `/posts/...` -> `social.linkedin.post`
3. System checks for LinkedIn cookies in the cookie store; if missing, returns a clear error with setup instructions
4. CookieFetcher retrieves cookies for `linkedin.com`, constructs authenticated HTTP client with realistic browser headers
5. CookieFetcher adds a random delay (1-5 seconds) before the request to mimic human browsing patterns
6. CookieFetcher fetches the page HTML; anti-scraping handler checks for rate limits, CAPTCHA, and bot detection
7. LinkedInProfileParser extracts structured data from the rendered HTML:
   a. Name, headline, location, connection count
   b. About/summary section
   c. Experience entries with company, title, dates, description
   d. Education entries with institution, degree, field, year
   e. Skills list
   f. Recommendations with recommender attribution
8. ExperienceDecomposer creates individual Sections for each experience and education entry, attaching structured metadata (company, dates, duration)
9. EntityResolver creates entities:
   - Person entity: `@linkedin.username` with name, headline
   - Company entities: `@org.company-name` for each employer
   - Institution entities: `@edu.institution-name` for each school
10. If the person is already known (e.g., captured their X/Twitter profile), links to canonical entity
11. Skills are extracted and mapped to tags in the vocabulary
12. Sectioner assembles Sections in display order: headline, about, experience (chronological), education, skills, recommendations
13. Tagger assigns additional tags based on content analysis
14. EmbeddingGenerator creates per-section embeddings plus full-profile embedding
15. Raw HTML snapshot stored alongside structured data for provenance
16. Job status updated to `completed`

### Configuration

```yaml
# In configuration.yaml
linkedin:
  # Authentication: cookie bridge only (no API available)
  authMethod: cookie_bridge

  # Rate limiting (aggressive to avoid detection)
  rateLimit:
    # Minimum delay between requests (seconds)
    minDelay: 3
    # Maximum additional random delay (seconds)
    maxRandomDelay: 7
    # Maximum requests per hour
    maxRequestsPerHour: 60
    # Backoff multiplier when rate limited
    backoffMultiplier: 3.0
    # Maximum backoff (seconds)
    maxBackoffSeconds: 600

  # Anti-scraping detection
  antiScraping:
    # Pause and notify on CAPTCHA detection
    captchaPause: true
    # Pause and notify on bot detection (HTTP 999)
    botDetectionPause: true
    # Notify method: "cli" (print warning) or "webhook" (send notification)
    notifyMethod: cli

  # Profile capture settings
  profile:
    captureExperience: true
    captureEducation: true
    captureSkills: true
    captureRecommendations: true
    captureAbout: true
    # Maximum recommendations to capture
    maxRecommendations: 20

  # Post capture settings
  post:
    captureComments: true
    maxComments: 50

  # Store raw HTML for provenance
  storeRawHTML: true

  # LinkedIn URL normalization
  domains:
    - linkedin.com
    - www.linkedin.com

  # Browser headers to mimic real browser
  headers:
    User-Agent: "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36"
    Accept-Language: "en-US,en;q=0.9"
```

---

## E2E Test Checklist

- [ ] CLI: `ctxt capture https://linkedin.com/in/username` returns job ID within 3 seconds
- [ ] CLI: `ctxt capture https://linkedin.com/posts/username_activity-123` returns job ID within 3 seconds
- [ ] CLI: `ctxt capture https://www.linkedin.com/in/username` normalizes www prefix
- [ ] Profile: Name, headline, location, and connection count captured
- [ ] Profile: About/summary section captured
- [ ] Profile: Experience entries captured with company, title, dates, and description
- [ ] Profile: Education entries captured with institution, degree, field, and year
- [ ] Profile: Skills list extracted and mapped to tags
- [ ] Profile: Recommendations captured with recommender attribution
- [ ] Post: Post text, author, timestamp, and engagement counts captured
- [ ] Post: Comments captured with author attribution
- [ ] Entity: LinkedIn person entity created with `@linkedin.username` identifier
- [ ] Entity: Company entities created for each employer
- [ ] Entity: Institution entities created for each school
- [ ] Entity: Same person captured twice resolves to same entity (idempotent)
- [ ] Entity: Person entity linked to canonical entity if known from another platform
- [ ] Auth: Capture works with valid LinkedIn cookies
- [ ] Auth: Missing cookies returns clear error with setup instructions
- [ ] Auth: Expired cookies detected and user notified to re-authenticate
- [ ] Anti-Scraping: CAPTCHA detection pauses capture and notifies user
- [ ] Anti-Scraping: HTTP 999 (bot detection) triggers pause and notification
- [ ] Anti-Scraping: Rate limiting (HTTP 429) triggers exponential backoff
- [ ] Anti-Scraping: Random delay between requests prevents pattern detection
- [ ] Provenance: Raw HTML snapshot stored alongside structured data
- [ ] REST API: `POST /api/v1/analyze` request payload contains `source_type: url`, `source_url`, `auth_method: cookie_bridge`, and `options` object with `capture_experience`, `capture_education`, `capture_skills`, and `capture_recommendations` fields
- [ ] REST API: `POST /api/v1/analyze` returns 202 with `job_id` and `object_id`
- [ ] Storage: Completed profile object stored with `source.auth_method: cookie_bridge`, `metadata.platform: linkedin.com`, `metadata.headline`, `metadata.experience_count`, and `metadata.skills_count` (verifiable via `GET /api/v1/objects/<id>`)
- [ ] Search: Profile headline searchable via `ctxt search "CEO at Microsoft"`
- [ ] Search: Experience content searchable via `ctxt search "cloud infrastructure"`
- [ ] Search: Skills searchable via `ctxt search "distributed systems"`
- [ ] Error: Invalid LinkedIn URL returns descriptive error
- [ ] Error: Non-existent profile returns clear "profile not found" error
- [ ] Pipeline: `social.linkedin.profile` completes all 7 steps in correct order
- [ ] Pipeline: `social.linkedin.post` completes all 6 steps in correct order

---

## Related Stories

- [US-0200](./US-0200-browser-cookie-bridge.md) -- Cookie bridge is required for LinkedIn capture (no public API)
- [US-0201](./US-0201-x-twitter-capture.md) -- Similar social platform capture pattern for X/Twitter
- [US-0002](../ingestion/US-0002-url-capture-and-extraction.md) -- Base URL capture; LinkedIn extends with platform-specific parsing
- [US-0009](../enrichment/US-0009-extract-entities-and-mentions.md) -- Entity extraction from profile content
- [US-0046](../enrichment/US-0046-extract-relationships-between-entities.md) -- Relationship extraction between professional entities
- [US-0047](../enrichment/US-0047-extract-temporal-information.md) -- Temporal extraction from career timelines

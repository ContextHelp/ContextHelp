# LinkedInStrategy

Single platform-keyed strategy for linkedin.com.

## Surfaces

| Host | Specificity |
|---|---|
| linkedin.com (apex) | 1 |
| any *.linkedin.com (www., business., learning.) | 2 |

## Sub-paths

- `/in/<slug>` — user profile + recent-activity facet.
- `/company/<slug>` — org page + people facet + posts facet.
- `/school/<slug>` — school page.
- `/posts/<id>` — single post; also emits author profile
  (`linkedin_profile`).
- `/pulse/<slug>` — long-form article (`linkedin_article`).

Unsupported paths (jobs/view/, learning/, events/, etc.) are
intentionally no-op until daemon-side fetcher gains those surfaces.

## Identity keys

| Type | Form |
|---|---|
| `linkedin_profile` | `linkedin/profile/<slug>` |
| `linkedin_org` | `linkedin/org/<slug>` |
| `linkedin_school` | `linkedin/org/school_<slug>` |
| `linkedin_post` | `linkedin/post/<raw_id>` |
| `linkedin_article` | `linkedin/article/<slug>` |

## Wiring

`linkedin.New()` takes no client. The strategy is purely
URL-structural: it parses the captured URL and emits sub-path probe
candidates. Identity keys are slug-keyed. A future patch may
re-introduce a daemon-side client for slug → stable-id resolution;
until it does, no wiring is required.

## Limits

- LinkedIn aggressively blocks unauthenticated scraping. The strategy
  emits canonical URLs; a daemon-side authenticated client (or a
  user's logged-in browser via capture integration) is required to
  actually hydrate them.
- Post URLs of the form `/posts/<author>_<activity-id>` are split on the
  last `_`. Authors whose slugs contain underscores will surface a
  malformed author handle in the secondary profile probe; the resolver
  drops these via identity-key uniqueness.

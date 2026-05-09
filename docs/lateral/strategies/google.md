# GoogleStrategy family

Five strategies share `strategies/google/`: a catch-all parent plus
specialised children for Search, Scholar, Trends, News.

## Surfaces

| Strategy | Host(s) | Path trigger | Specificity |
|---|---|---|---|
| `GoogleStrategy` | google.com, *.google.com, *.gle | any | 1 (apex) / 2 (subdomain) |
| `GoogleSearchStrategy` | google.com | `/search` | 3 |
| `GoogleScholarStrategy` | scholar.google.com | any | 3 |
| `GoogleTrendsStrategy` | trends.google.com | any | 3 |
| `GoogleNewsStrategy` | news.google.com | `/articles/<id>`, `/topics/<id>` | 3 |

## Sub-paths

- **Search**: query candidate (`google_query`) plus the top organic
  results when `GoogleClient.SearchTopResults` returns them
  (`google_result`, capped at `ResultCap=5`).
- **Scholar**: `/citations?user=<id>` → author (`scholar_author`);
  `/scholar?cluster=<id>` → paper (`scholar_paper`); `/scholar?q=<q>` →
  query candidate plus author landing if client resolves.
- **Trends**: `/trends/explore?q=<topic>` → primary trend plus
  client-supplied related queries (`trends_topic`).
- **News**: `/articles/<id>` → article candidate; `/topics/<id>` → topic
  plus client-supplied top articles (`news_article`, capped at
  `ArticleCap=5`).
- **Parent fallback**: a single `google_generic` candidate keyed by
  host+path so the resolver still dedups repeated captures of the same
  Google surface (Forms, Drive, Calendar, etc.).

## Identity keys

| Type | Form |
|---|---|
| `google_query` | `google/search/<query>` |
| `google_result` | `google/search/<query>\|<host_path>` |
| `scholar_author` | `scholar/profile/<user_id>` |
| `scholar_paper` | `scholar/paper/<cluster_id>` |
| `trends_topic` | `trends/trend/<topic>` |
| `news_article` | `news.google/article/<id>` |
| `news_topic` | `news.google/topic/<id>` |
| `google_generic` | `google/page/<host>/<path>` |

## Wiring

Daemon supplies one `google.GoogleClient` covering all four child
methods. Pass `nil` to skip follow-up fetches; strategies still produce
the URL-only candidates (query / topic / etc.) keyed for resolver dedup.

## Limits

- Specificity scoring caps at 3; new specialised google children must
  reuse 3 (not 4) so dispatch ordering remains deterministic across
  multiple google children that match the same URL.
- `GoogleStrategy` parent emits a single generic candidate. It is
  intentionally minimal — exists only so unhandled Google surfaces
  still flow through the lateral pipeline.
- Per-strategy config gates live under `lateral.strategies.<name>` in
  the substrate config.

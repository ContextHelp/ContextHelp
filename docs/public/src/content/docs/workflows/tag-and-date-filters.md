---
title: "Workflow: Tag and Date Filters"
description: Select knowledge by several tags and a creation window with one grammar on list, find and compose.
---

## Goal

Pull the knowledge of a tag group over a time window in one call: `ctxt list`, `ctxt find` and `ctxt compose` take the same `--tagged` grammar and the same date flags.

```bash
ctxt compose brief --tagged +a,b,+c --since 30d
ctxt list --tagged a --tagged b --since 1w
ctxt find "deploy" --tagged ux,design --since yesterday
```

## `--tagged`

A list of tags, written as a comma list, as a repeated flag, or both. Values concatenate in order; duplicates collapse, and a required mark wins over a plain one. Tags need no quoting, except labels that hold spaces.

A leading `+` marks a tag **required**; a plain tag is **optional**.

| Written | Selects |
|---|---|
| `--tagged a` | objects carrying `a` |
| `--tagged a,b,c` | objects carrying `a`, `b` **or** `c` (any: with no `+` tag, one plain tag must match) |
| `--tagged a --tagged b` | the same as `a,b` |
| `--tagged +a,+b,+c` | objects carrying `a` **and** `b` **and** `c` (all required) |
| `--tagged +a,b,+c` | `a` and `c` required; `b` optional: it never excludes, it only ranks |

- With any `+` tag, every `+` tag must match and the plain tags only rank. With none, at least one plain tag must match.
- Only commas separate tags: a label may hold spaces (`--tagged "machine learning"`), and a `+` past the first character is part of the label (`c++`). Matching is case-sensitive: `UX` and `ux` are different tags.
- An unknown tag selects nothing (an empty result, not an error).
- A leading `-` is reserved for excluding a tag later and is refused now (`exclusion not supported yet`).

Errors (exit 2, usage): an empty tag (`a,,b`, `--tagged ""`); a bare `+` with no label; a leading `-`.

On REST, `+` is the required marker and is read raw: `?tagged=+a,b` and `?tagged=%2Ba,b` both mean `a` required and `b` optional. A space inside a label is `%20` (a raw `+` inside a label stays a plus). Repeated `tagged` parameters concatenate in order.

### Ranking

Results matching more of the listed tags rank first: every listed tag an object carries counts one, whether it is required (`+`) or plain. An object matching 3 tags always ranks above one matching 2, which ranks above one matching 1, whichever tags they are.

| Command | Order |
|---|---|
| `list`, `compose` | tags matched (more first), then the usual sort (`--sort`, `--dir`, created newest first by default), then id; `--start`/`--limit` page that order and `total` counts every match |
| `find` | tags matched (more first), then relevance. The count is a tier, not a score bonus: relevance scores are unchanged, `--min-score` reads relevance alone, and `--explain` shows the count as `tags_matched` per result |

The required tags always match, so beside them only the plain tags differentiate; with no `+` tag every listed tag does. A single tag, or a list of `+` tags only, cannot reorder anything.

`list --cursor` and `list --changed-after` walk a fixed order (creation, change), so they refuse a `--tagged` that ranks: two or more tags with no `+` tag, or a plain tag beside a `+` tag (usage error, exit 2; REST `changed_after` answers 400 on `tagged`). One tag, or only `+` tags, works with both.

The full-text and vector legs of `find` filter by the tags and read their candidate pools in relevance order; the tier applies after, so a pool cut never drops a relevant hit for carrying fewer tags.

The filter runs in the store (SQLite and Postgres): tag `IN` for any, one `EXISTS` per required tag for all. Totals and pages are exact.

## Dates

`--since` and `--until` bound the **created** date on `list`, `find` and `compose`. The range is half-open: `--since` is inclusive, `--until` exclusive (created before it). Accepted values:

| Value | Meaning |
|---|---|
| `3d`, `42h`, `1w`, `30m`, `1M`, `1y` | now minus the span. Units: `m` or `min` = **minute**, `h` hour, `d` day, `w` week, `M` or `mo` = **month**, `y` year. Case matters: `1m` is one minute, `1M` or `1mo` one month |
| `yesterday`, `today` | the whole local day: a start bound is its first instant, an end bound the start of the next day |
| `2026-01-31` | the whole local day, likewise: `--until 2026-01-31` includes the 31st |
| `2026-01-31T18:00:00Z` | the instant, any RFC 3339 offset |
| `2026-01-31T18:00:00` | the instant, in the local time zone |

The dates mentioned in an object's content have their own flags, `--mentioned-since` and `--mentioned-until`, taking the same values (resolved to a day).

## REST and MCP

The same grammar, one string:

- `GET /api/v1/objects` and `/objects/facets`: `tagged=+a,b,+c`, `since`, `until`, `mentioned_since`, `mentioned_until`.
- `POST /api/v1/find`: `filter.tagged`, `filter.since`, `filter.until`, `filter.mentioned_since`, `filter.mentioned_until`.
- MCP `search` tool: `tagged` (a string, or an array of strings joined in order), `since`, `until`, `mentioned_since`, `mentioned_until`.

`tagged` may repeat on REST, as on the CLI; values join in order. Servers take absolute dates only (RFC 3339 or `YYYY-MM-DD`, UTC); the CLI resolves relative values before sending. A date-only `until` takes that whole UTC day (the server reads it as the start of the next day, exclusive), a date-only `since` starts at its midnight; `mentioned_*` compare whole days, both inclusive. Details: REST API reference.

## Related

- [`search-retrieval.md`](./search-retrieval.md)
- [`composition-reporting.md`](./composition-reporting.md)
- [`mcp-agents.md`](./mcp-agents.md)

# WikipediaStrategy

Single platform-keyed strategy for any *.wikipedia.org language edition.

## Surfaces

| Host | Specificity |
|---|---|
| wikipedia.org (apex, redirects to en.) | 1 |
| any *.wikipedia.org (en., fr., ja., …) | 2 |

## Sub-paths

For a captured `/wiki/<title>` URL:

- `wiki_article` — canonical article URL.
- `wiki_talk` — `/wiki/Talk:<title>` discussion page.
- `wiki_history` — `/w/index.php?title=<title>&action=history`.
- `wiki_categories` — `/wiki/Special:WhatLinksHere/<title>` (backlinks).
- `wiki_interwiki` — `langlinks` API anchor for cross-language traversal.
- `wiki_wikidata` — Q-id page when `WikipediaClient.ResolveWikidata`
  resolves it.

Pseudo-namespaces (Special:, File:, Help:, Wikipedia:, Portal:,
Category:, Talk:, User:, User_talk:) skip the probe — they aren't
articles.

## Identity keys

Locale-aware so `en/Turing_machine` and `fr/Machine_de_Turing` do not
collide before the resolver merges them via the Wikidata probe.

| Type | Form |
|---|---|
| All article facets | `wikipedia/article/<lang>/<title>` |
| `wiki_wikidata` | `wikidata/item/<qid>` |

## Wiring

`wikipedia.New(client)` takes a `WikipediaClient.ResolveWikidata(lang,
title)`. Optional; nil client skips the wikidata candidate.

## Limits

- Locale detection is naive: takes the leftmost label of the host. The
  `m.` mobile and `commons.` / `species.` non-language hosts will
  surface an unusual lang segment in the identity key. The daemon's
  resolver merges via the Wikidata probe when present.

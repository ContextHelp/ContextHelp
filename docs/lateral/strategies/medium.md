# MediumStrategy family

Three strategies for Medium: parent catch-all + Publication child +
Profile child. Custom-domain Medium publications resolve via the
shared `customdomain` detector.

## Surfaces

| Strategy | Match | Specificity |
|---|---|---|
| `MediumStrategy` (parent) | medium.com, *.medium.com, custom Medium | 1 (apex/custom) / 2 (subdomain) |
| `MediumPublicationStrategy` | medium.com/<slug>, *.medium.com root, custom-domain root | 3 |
| `MediumProfileStrategy` | medium.com/@<username> | 3 |

## Sub-paths

- **Publication**: landing, archive, RSS feed candidates. Custom-domain
  captures resolve to a slug via `MediumClient.ResolvePublication(host)`
  when wired; otherwise the host itself becomes the slug.
- **Profile**: `/@<user>` landing, `/@<user>/following`, RSS feed.
- **Parent**: article (when `/p/<id>` or `/<pub>/<slug>`) plus author
  (when `/@user/...`) plus RSS feed.

## Identity keys

| Type | Form |
|---|---|
| `medium_article` | `medium/article/<slug>` |
| `medium_author` | `medium/profile/<username>` |
| `medium_publication` | `medium/publication/<slug>` |
| `medium_feed` | `medium/feed/<id>` |

## Wiring

`medium.NewParent / NewPublication / NewProfile` each take a
`MediumClient` (optional). Methods used today:

- `ResolvePublication(host) → slug` — custom-domain mapping.
- `ResolveAuthor(articleURL) → username` — defined but not yet
  consulted; reserved for an article-from-publication probe.

## Limits

- Path-based publication detection (`medium.com/<slug>`) collides with
  paths like `/me/stories` or `/about`. The strategy currently treats
  any single-segment path that doesn't start with `@` and isn't `p` as
  a publication slug; refinement is deferred.
- Custom-domain detection without page-side hints (Generator,
  CanonicalHost, MetaPlatform) returns Unknown — strategies do not
  match such captures. The capture pipeline must surface those hints
  for custom-domain Medium publications to participate.

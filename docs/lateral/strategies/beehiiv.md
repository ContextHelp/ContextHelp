# BeehiivStrategy family

Three strategies for Beehiiv: parent catch-all + Publication child +
Post child. Custom-domain Beehiiv publications resolve via the shared
`customdomain` detector.

## Surfaces

| Strategy | Match | Specificity |
|---|---|---|
| `BeehiivStrategy` (parent) | *.beehiiv.com + custom-domain | 1 (custom) / 2 (canonical) |
| `BeehiivPublicationStrategy` | root, `/archive`, `/about`, `/subscribe` | 3 |
| `BeehiivPostStrategy` | `/p/<slug>` | 3 |

## Sub-paths

- **Publication**: landing, `/archive`, `/about`, `/feed`.
- **Post**: post landing, parent publication, `/archive` anchor.
- **Parent**: publication landing + RSS feed.

## Identity keys

| Type | Form |
|---|---|
| `beehiiv_publication` | `beehiiv/publication/<slug>` |
| `beehiiv_post` | `beehiiv/post/<slug>/<post-slug>` |
| `beehiiv_archive` | `beehiiv/publication/<slug>/archive` |
| `beehiiv_feed` | `beehiiv/feed/<slug>` |

## Wiring

Each constructor takes an optional `BeehiivClient.ResolvePublication`
for custom-domain → slug resolution.

## Limits

- Beehiiv has no Notes/Posts-of-author traversal in v1 — the strategy
  surfaces only publication-scoped probes. Per-author lateral on Beehiiv
  is deferred.
- Same custom-domain hint requirement as Substack/Medium: the capture
  pipeline must thread page-side signals.

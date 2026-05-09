# SubstackStrategy family

Four strategies for Substack: parent catch-all + Publication child +
Post child + Notes child. Custom-domain Substack publications resolve
via the shared `customdomain` detector.

## Surfaces

| Strategy | Match | Specificity |
|---|---|---|
| `SubstackStrategy` (parent) | *.substack.com + custom-domain | 1 (custom) / 2 (canonical) |
| `SubstackPublicationStrategy` | root path or `/archive` | 3 |
| `SubstackPostStrategy` | `/p/<slug>` | 3 |
| `SubstackNotesStrategy` | substack.com `/notes`, `/note/<id>`, `/profile/<id>/note/<noteid>` | 3 |

## Sub-paths

- **Publication**: landing, `/archive`, `/about`, `/feed`.
- **Post**: `/p/<slug>` post, parent publication, `/p/<slug>/comments`.
- **Notes**: global `/notes` feed, individual `/note/<id>`, profile-scoped
  notes (which also emit author profile).
- **Parent**: publication landing + RSS feed for unmatched paths.

## Identity keys

| Type | Form |
|---|---|
| `substack_publication` | `substack/publication/<slug>` |
| `substack_post` | `substack/post/<slug>/<post-slug>` |
| `substack_note` | `substack/note/<id>` (or `<profile>/<noteid>`) |
| `substack_author` | `substack/profile/<profile>` |
| `substack_feed` | `substack/feed/<slug>` |

## Wiring

Each constructor takes an optional `SubstackClient.ResolvePublication`
for custom-domain → slug resolution. When unset, the host becomes the
slug (URL form falls back to `https://<host>`).

## Limits

- Custom-domain captures without `customdomain` hints (Generator /
  CanonicalHost / MetaPlatform) fail the parent's `Applies` check. The
  capture pipeline must thread page-side hints through a future
  CapturedEvent extension before fully unattended custom-domain
  Substack lateral works.
- Notes URLs are scoped to substack.com only (notes don't appear on
  publication subdomains in Substack's current routing).

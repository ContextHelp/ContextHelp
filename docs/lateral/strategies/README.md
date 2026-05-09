# Lateral capture — platform strategy roster

The P4 platform roster ships ten platform-keyed parents plus nine
specialised children plus two cross-cutting helpers. Every platform
strategy lives at `internal/lateral/strategies/<platform>/` and follows
the same shape laid out in
[../../superpowers/specs/2026-05-08-lateral-capture-discovery-design-amendment-1.md](../../superpowers/specs/2026-05-08-lateral-capture-discovery-design-amendment-1.md).

## Roster

| Strategy | Package | Surfaces |
|---|---|---|
| GoogleStrategy | `strategies/google` | google.com catch-all + .gle |
| GoogleSearchStrategy | `strategies/google` | google.com/search |
| GoogleScholarStrategy | `strategies/google` | scholar.google.com |
| GoogleTrendsStrategy | `strategies/google` | trends.google.com |
| GoogleNewsStrategy | `strategies/google` | news.google.com |
| XStrategy | `strategies/x` | x.com, twitter.com |
| LinkedInStrategy | `strategies/linkedin` | linkedin.com |
| ArxivStrategy | `strategies/arxiv` | arxiv.org |
| WikipediaStrategy | `strategies/wikipedia` | *.wikipedia.org |
| MediumStrategy | `strategies/medium` | medium.com + custom |
| MediumPublicationStrategy | `strategies/medium` | publication homepage |
| MediumProfileStrategy | `strategies/medium` | /@username |
| SubstackStrategy | `strategies/substack` | *.substack.com + custom |
| SubstackPublicationStrategy | `strategies/substack` | publication root |
| SubstackPostStrategy | `strategies/substack` | /p/<slug> |
| SubstackNotesStrategy | `strategies/substack` | substack.com/notes |
| BeehiivStrategy | `strategies/beehiiv` | *.beehiiv.com + custom |
| BeehiivPublicationStrategy | `strategies/beehiiv` | publication root |
| BeehiivPostStrategy | `strategies/beehiiv` | /p/<slug> |
| YouTubeStrategy | `strategies/youtube` | youtube.com, youtu.be |

Plus shared:

- `strategies/customdomain` — Medium/Substack/Beehiiv custom-domain detector.
- `strategies/identitykey` — `Preview["identity_key"]` conventions.
- `strategies/roster` — `Register(reg, gates, deps)` aggregator + cassette/integration tests.

## Specificity calibration

Within the platform family the dispatcher keeps only the highest-specificity
match (children shadow parents). Calibration:

- Apex/canonical host: 1
- Subdomain or path-based child trigger: 2
- Specialised child handling its dedicated subdomain or path prefix: 3

## Identity-key contract

Every candidate carries `Preview["identity_key"]` — the resolver's dedup
handle. Format: `<platform>/<entity_type>/<id>`. See
[strategies/identitykey/identitykey.go](../../../internal/lateral/strategies/identitykey/identitykey.go)
for the full vocabulary.

## Per-platform pages

- [google.md](google.md)
- [x.md](x.md)
- [linkedin.md](linkedin.md)
- [arxiv.md](arxiv.md)
- [wikipedia.md](wikipedia.md)
- [medium.md](medium.md)
- [substack.md](substack.md)
- [beehiiv.md](beehiiv.md)
- [youtube.md](youtube.md)

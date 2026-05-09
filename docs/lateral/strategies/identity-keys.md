# Identity Keys

The cross-strategy contract for candidate dedup. Every `lateral.Candidate` that
points at an identifiable real-world entity (person, repo, publication, paper,
video, organisation) carries an `identity_key` string in `Candidate.Preview`.
The substrate's identity resolver uses equality on this string to merge
candidates that point at the same entity even when their URLs differ
(shortlinks, mobile hosts, custom domains, locale prefixes).

Package: `internal/lateral/strategies/identitykey`

## Format

```
<platform>/<entity_type>/<id_part_1>[/<id_part_2>...]
```

For locale-scoped entities (Wikipedia language editions):

```
<platform>/<entity_type>/<locale>/<id_part_1>[/<id_part_2>...]
```

All segments are lowercased and trimmed. Slashes embedded in any single id
segment escape to underscores so the resulting key remains parseable.

## Examples

| Strategy | Identity key |
| --- | --- |
| GitHub repo | `github/repo/samber/lo` |
| GitHub user | `github/owner/jadb` |
| YouTube channel | `youtube/channel/uc12345` |
| YouTube uploads playlist | `youtube/channel/uc12345/uploads` |
| Substack publication | `substack/publication/anthropic-research` |
| arXiv paper | `arxiv/paper/2401.12345` |
| Wikipedia article (locale-scoped) | `wikipedia/article/en/turing_machine` |

## Wiring strategies

```go
import "github.com/ideacrafterslabs/ctxt/internal/lateral/strategies/identitykey"

candidate.Preview = identitykey.Set(candidate.Preview, identitykey.Build(
    "github", identitykey.EntityRepository, owner, repo,
))
```

Multi-segment ids are first-class: pass each segment as a separate variadic
argument. Don't pre-concatenate (`"owner/repo"`) — that triggers escaping and
you get `owner_repo`, not `owner/repo`.

```go
// Wrong: slash escapes to underscore
identitykey.Build("github", "repo", owner+"/"+repo)
// → "github/repo/samber_lo"  (different key from below)

// Right: variadic id segments
identitykey.Build("github", "repo", owner, repo)
// → "github/repo/samber/lo"
```

## Reading on the resolver side

```go
key := identitykey.Get(candidate.Preview)
parsed := identitykey.Parse(key, /* localised = */ false)
// parsed.Platform, parsed.EntityType, parsed.IDParts
```

`Parse` cannot tell a localised key from a non-localised one purely by shape
(both look the same). The caller passes `localised` explicitly because only the
platform itself knows. Wikipedia callers pass `true`; everyone else passes
`false`.

## Empty / missing inputs

- Empty or whitespace-only id segments are dropped silently. Pass at least one
  non-empty segment to produce a usable key.
- `HostBackedID(rawURL)` returns `""` when `rawURL` has no host. **Callers
  MUST guard against this** — passing a `""` result through to `Build` collapses
  the key to just `<platform>/<entity>`, which is non-unique across captures.
  Skip the candidate or use a stable fallback id when `HostBackedID` returns
  empty.

## Entity-type vocabulary

The package exports constants for the common entity types
(`EntityRepository`, `EntityUser`, `EntityArticle`, ...). New entries are
additive — the resolver treats unknown entity types as opaque, so adding a
new constant doesn't break existing keys. Prefer the constants over string
literals so a typo doesn't silently produce a non-equal key.

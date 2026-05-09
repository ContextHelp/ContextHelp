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

Strategies set the typed `Candidate.IdentityKey` field directly at
construction time:

```go
import "github.com/ideacrafterslabs/ctxt/internal/lateral/strategies/identitykey"

cand := lateral.Candidate{
    URL:           url,
    CandidateType: "owner_repo",
    Strategy:      ID,
    IdentityKey:   identitykey.Build("github", identitykey.EntityRepository, owner, repo),
    Preview:       map[string]any{"owner": owner, "name": repo},
}
```

The legacy `identitykey.Set(preview, key)` form remains supported for
one minor cycle (resolver dual-reads typed field then Preview entry)
but is `Deprecated:` for new code. See Amendment 2
(`docs/superpowers/specs/2026-05-09-lateral-capture-discovery-design-amendment-2.md`)
for the migration semantics.

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

The substrate's identity resolver (`internal/lateral/identity`) consumes
identity keys via `identitykey.Get(candidate.Preview)` and applies them
ahead of URL equality. Three rules govern the consumer contract; tests
that pin them live in `internal/lateral/identity/{precedence,fallback,
dedup,property}_test.go`.

### Precedence rule — identity_key wins over URL

When a candidate carries a non-empty identity_key AND the graph holds a
matching entry, the resolver returns that canonical regardless of URL
match. This is what lets dedup survive URL skew (mobile host, custom
domain, locale prefix).

```go
import "github.com/ideacrafterslabs/ctxt/internal/lateral/strategies/identitykey"

key := identitykey.Get(c.Preview)
if key != "" {
    if id, err := graph.FindByIdentityKey(ctx, key); err == nil {
        return Result{EdgeOnly: true, CanonicalID: id}, nil
    }
    // ErrNotFound falls through to URL match; other errors propagate.
}
id, err := graph.FindByURL(ctx, c.URL)
// ...
```

### Fallback rule — empty identity_key falls back to URL

`identitykey.Get` returns `""` when `Preview` is nil, the entry is
missing, the value is the empty string, or the value isn't a string.
Each case skips the identity_key path and consults URL equality —
matching pre-identitykey behaviour for legacy candidates and degenerate
strategy outputs (per `Build`'s "refuse to emit a non-unique key"
contract).

### Mismatch rule — distinct identity_keys never merge under one canonical

Two candidates with different non-empty identity_keys MUST resolve to
distinct canonicals — even when their URLs are equal. The graph's
`FindByIdentityKey` is the source of truth for "is this entity already
known"; URL match never bridges two known-different keys.

### Key-not-yet-in-graph fallback

A non-empty identity_key that returns `ErrNotFound` from
`FindByIdentityKey` means "the graph hasn't indexed this key yet."
Resolver falls through to URL match: if the URL is in the graph (e.g.
the entity was first captured before identity_key was emitted), the
candidate dedups under the URL canonical. If both miss, the candidate
becomes probationary.

The two rules above don't conflict — the mismatch rule applies to
candidates the graph already knows under different keys; the
key-not-yet-in-graph rule applies to candidates the graph hasn't seen
under the proposed key at all.

### Reading the structured key

When the resolver (or an adjacent component) needs the segments rather
than just the opaque string:

```go
key := identitykey.Get(candidate.Preview)
parsed := identitykey.Parse(key, /* localised = */ false)
// parsed.Platform, parsed.EntityType, parsed.IDParts
```

`Parse` cannot tell a localised key from a non-localised one purely by
shape (both look the same). The caller passes `localised` explicitly
because only the platform itself knows. Wikipedia callers pass `true`;
everyone else passes `false`.

### Typed `Candidate.IdentityKey` (landed)

Substrate Amendment 2 (T-0307 / T-0309) promotes `identity_key` from a
`Preview` entry to a typed `Candidate.IdentityKey` field. The resolver
reads the typed field first; when empty, it falls back to
`identitykey.Get(c.Preview)`. Strategies emit the typed field directly
via `Candidate{IdentityKey: identitykey.Build(...)}`; the helper
`identitykey.Of(c lateral.Candidate)` wraps the dual-read for adapters
that bridge `lateral.Candidate` to `identity.Candidate`.

When both forms are set with non-empty conflicting values, the typed
field wins — by intent. The typed field is the contract; Preview is
the legacy back-channel.

The precedence, fallback, and mismatch rules above stay unchanged
across the transition. After one minor cycle the deprecated
`identitykey.Set` and the Preview-map fallback in the resolver are
candidates for removal.

## Empty / missing inputs

`Build` returns `""` (empty string) under any of these conditions, signalling
"refuse to emit a non-unique key":

- `platform` is empty or whitespace-only.
- `entityType` is empty or whitespace-only.
- All `id` segments are empty or whitespace-only (i.e. no usable id remains
  after normalisation).

`BuildLocalised` returns `""` under the same conditions, plus when `locale`
is empty (a locale-scoped key without a locale is malformed).

Callers MUST treat `""` as "skip the candidate" or "don't set Preview's
identity_key field." Emitting a candidate without an identity_key forces the
resolver to fall back to URL-equality dedup — which is the correct degraded
behaviour. Emitting a candidate with `"<platform>/<entity>"` (no id) would let
the resolver merge unrelated entities under one key.

```go
key := identitykey.Build("github", identitykey.EntityRepository, owner, repo)
if key == "" {
    return // skip — owner or repo wasn't resolvable
}
candidate.Preview = identitykey.Set(candidate.Preview, key)
```

## Host-backed fallback IDs

When a strategy doesn't have a structural canonical id (e.g. unknown CMS,
generic search-result URLs), `HostBackedIDParts(rawURL)` returns a `[]string`
of `[host, pathPart1, pathPart2, ...]` derived from the URL. Spread the slice
into `Build`'s variadic id:

```go
parts := identitykey.HostBackedIDParts(url)
if parts == nil {
    return // skip — URL had no host
}
key := identitykey.Build("google", identitykey.EntityArticle, parts...)
```

`HostBackedIDParts` returns `nil` when the URL is unparseable or has no host.
Spreading `nil` into `Build` yields `""` by Build's contract, but checking at
the call site lets the strategy decline the candidate entirely (preferred)
rather than emit one without an identity_key.

Returning `[]string` instead of a single `host/path/...` string is intentional:
if it returned a single concatenated string, callers would face a choice —
pass it as one id segment (slashes escape to underscores → `host_path_part`)
or split and pass as multiple (slashes preserved → `host/path/part`). Two
different keys for the same logical input. The `[]string` API forces the
correct multi-segment representation.

## Entity-type vocabulary

The package exports constants for the common entity types
(`EntityRepository`, `EntityUser`, `EntityArticle`, ...). New entries are
additive — the resolver treats unknown entity types as opaque, so adding a
new constant doesn't break existing keys. Prefer the constants over string
literals so a typo doesn't silently produce a non-equal key.

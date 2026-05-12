---
title: Lateral Capture Discovery — Amendment 2 (substrate followups)
date: 2026-05-09
status: amendment
amends: 2026-05-08-lateral-capture-discovery-design.md
---

# Lateral Capture Discovery — Amendment 2

## Why this amendment

P3 (GitHub family) and P4 (custom-domain platforms — medium / substack /
beehiiv) shipped working strategies, but each carried at least one
"substrate has no field for X" workaround:

1. **Identity key** lived as `Preview["identity_key"]` because
   `lateral.Candidate` had no typed field. Every strategy had to
   round-trip the key through a `map[string]any`, and a stray
   `Preview` overwrite could silently break dedup.

2. **Custom-domain platform attribution** depended on caller-supplied
   `Hints` (meta `generator`, explicit `MetaPlatform`, rel=canonical
   host) but `lateral.CapturedEvent` had no field to carry them. The
   medium / substack / beehiiv strategies declared `hintsFromEvent
   (ev) Hints { return Hints{} }` — an explicit "we know this is a
   workaround" stub.

3. **Sticky author hints** (e.g. "this captured GitHub PR was
   authored by samber, so probe their other PRs") were deferred at
   P3 review because `ActiveContext` had no per-platform author
   field. The probe code carried a `authorHintFor(ev) string {
   return "" }` stub.

This amendment promotes all three workarounds to first-class typed
fields on the substrate types and pins the migration semantics.

Where this amendment is silent, the original design + Amendment 1
apply.

## Substrate shape changes

### `CapturedEvent.Hints`

```go
package lateral

type Hints struct {
    Generator     string
    MetaPlatform  string
    CanonicalHost string
}

type CapturedEvent struct {
    ObjectID        string
    Namespace       string
    SourceURL       string
    CapturePipeline string
    PersistedAt     int64
    Hints           Hints  // new
}
```

The substrate (lateral package) owns the `Hints` type. Strategies
read it via `CapturedEvent.Hints` and forward to
`customdomain.Detect`. `customdomain.Hints` becomes a deprecated
alias of `lateral.Hints` for one minor cycle so existing callers
keep compiling without code changes.

**Backward compat**: zero `Hints` (the field's default value)
preserves pre-amendment behaviour. Custom-domain detection declines
when no signal is set; canonical-host suffix matching still runs.
The capture pipeline populates Hints when it has them; legacy
captures keep their existing fall-through path.

### `Candidate.IdentityKey`

```go
package lateral

type Candidate struct {
    URL           string
    CandidateType string
    Strategy      string
    Preview       map[string]any
    IdentityKey   string  // new
}
```

Mirrors on `identity.Candidate` (the resolver-input type) so the
resolver can dual-read.

**Migration semantics — typed field wins**: the resolver reads
`c.IdentityKey` first; if non-empty, uses it. If empty, falls back
to `Preview[identitykey.KeyField]`. When both are set with
conflicting non-empty values, the typed field wins. This makes the
sweep safe — a stale Preview entry can't override a freshly-set
typed key.

**One-cycle backward compat**: `identitykey.Set(preview, key)`,
`identitykey.Get(preview)`, and the per-package `ExtractIdentityKey`
helpers stay working. New helpers `identitykey.Of(c)` and
`identitykey.SetField(c, key)` operate on the typed field directly.
`Set` is marked `Deprecated:` in godoc; T-0309's strategy sweep
moved every in-tree call site onto the typed field.

### `ActiveContext.AuthorHints`

```go
package lateral

type ActiveContext struct {
    SessionTopic     map[string]float64
    CaptureWindow    map[string]float64
    InterestRegistry map[string]float64
    Fingerprint      string
    AuthorHints      map[string]string  // new
}
```

Keys are platform IDs (`"github"`, `"x"`, `"linkedin"`, ...); values
are platform-specific author identifiers. The capture pipeline's
session middleware populates the map when it has resolved an
authorial signal for the captured page.

**Backward compat**: nil / missing key = "no author hint for that
platform." Strategies treat the absence as "skip the author-scoped
probe." The github family is wired to read
`ActiveContext.AuthorHints["github"]` for `author_other_pr` and
`author_other_issue` (T-0308); other platforms wire as their
deferred work resumes.

## Test patterns

### Hints round-trip (T-0310)

For every custom-domain platform strategy:

- Negative: `CapturedEvent{SourceURL: customURL}` (no Hints) — the
  parent strategy must NOT match on a non-canonical host.
- Positive (MetaPlatform): `CapturedEvent{SourceURL: customURL,
  Hints: Hints{MetaPlatform: "<platform>"}}` — the parent matches
  and `Probe` emits non-empty `IdentityKey`s.
- Positive (Generator / CanonicalHost): same shape, exercising the
  meta-tag and rel=canonical signal pathways respectively.

Tests live alongside their strategy package
(`strategies/{substack,medium,beehiiv}/*_test.go`).

### IdentityKey precedence (T-0307)

In `internal/lateral/identity/resolver_test.go`:

- Typed field reaches `FindByIdentityKey` (mirror of the
  Preview-map test).
- Typed field wins over conflicting Preview entry.
- Empty typed field falls back to Preview entry (T-0309
  backward-compat).
- Substrate-level equivalence: a candidate that sets either form
  produces identical `Resolver.Resolve` output across a vector of
  representative keys.

### AuthorHints seam (T-0308)

In `strategies/github/probe_test.go`:

- `AuthorHints["github"] = "samber"` → `ListAuthoredPRs` /
  `ListAuthoredIssues` is called and `author_other_pr` /
  `author_other_issue` candidates surface.
- Empty / missing `AuthorHints["github"]` → those API calls are
  skipped and the corresponding candidate types do not appear in
  the probe result.

## Migration sweep summary

T-0309 swept 11 strategy packages onto `Candidate.IdentityKey`:

| Package | Files touched |
| --- | --- |
| github | `probe.go`, `advisory.go`, `identity.go` (factories) |
| google | `parent.go`, `search.go`, `scholar.go`, `news.go`, `trends.go` |
| medium | `parent.go`, `profile.go`, `publication.go` |
| substack | `parent.go`, `post.go`, `notes.go`, `publication.go` |
| beehiiv | `parent.go`, `post.go`, `publication.go` |
| wikipedia | `wikipedia.go` |
| x | `x.go` |
| linkedin | `linkedin.go` |
| arxiv | `arxiv.go` |
| youtube | `youtube.go` |
| identitykey | `identitykey.go` (new helpers `Of` / `SetField`, deprecation note on `Set`) |

JIT (`strategies/jit`) doesn't emit identity keys today (its
`Emit` writes only `page_type` + `body_size` to Preview); no
migration was required there.

The roster integration test
(`strategies/roster/integration_test.go`) reads either form so
external strategy authors can migrate at their own pace.

## Out of scope

This amendment ships the substrate seams. The following deferred
work resumes in separate tracks:

- Wiring `AuthorHints` for non-github platforms (x, linkedin,
  ...) — T-0308 demonstrates the seam on github only.
- The capture pipeline's session middleware for actually resolving
  per-platform author identifiers from a captured page — daemon
  work, not substrate.
- Removing the deprecated `identitykey.Set` after one minor cycle
  — gated on external consumers having migrated.
- Removing `customdomain.Hints` (the alias) after the same cycle.

## References

- T-0306 — Hints field
- T-0307 — IdentityKey field
- T-0308 — AuthorHints field
- T-0309 — strategy migration sweep
- T-0310 — Hints round-trip test suite
- Companion: `docs/lateral/strategies/identity-keys.md` (typed
  field section + transition notes)

# GitHub family — lateral discovery strategies

Tier-A platform-keyed strategies rooted at `github.com`. Three
strategies ship in P3:

| Strategy | ID | Family | Specificity | Domain |
|----------|------|--------|-------------|--------|
| `GitHubStrategy` | `github` | platform | 1 (parent) | `github.com` (excluding `gist.github.com` + `/advisories/*`) |
| `GistStrategy` | `github.gist` | platform | 2 (child) | `gist.github.com` |
| `SecurityAdvisoryStrategy` | `github.advisory` | platform | 2 (child) | `github.com/advisories/*`, `github.com/<o>/<r>/security/advisories/*` |

Children shadow the parent: when both register and a captured URL
matches a child surface, the dispatcher selects only the child.
Property tests in `specificity_test.go` enforce this invariant under
both registration orders.

## Overview

GitHub captures land in lateral.discover via the substrate's
`ctxt.ingest.object.persisted` subscription. The github family claims
those captures whose `SourceURL` is on `github.com` (or a recognised
sub-host) and dispatches sub-path probes that fan out fetches against
the github API.

Sub-path classification is path-structural — pageshape is **not**
consulted because github's surface is fully specified by URL shape:
`/<owner>` is a profile, `/<owner>/<repo>` is a repo, etc.

## Sub-path catalog

### `GitHubStrategy` (`github`)

| URL shape | Page type | Candidates emitted |
|-----------|-----------|--------------------|
| `/<owner>/<repo>` | repo | `sibling_repo`, `owner_profile`, `pinned_repo`, `sponsor_page`, `starred_repo` |
| `/<owner>/<repo>/pull/<n>` | pull request | `owner_profile`, `sibling_repo`, `pr_reviewer`, `author_other_pr` (deferred behind author hint) |
| `/<owner>/<repo>/pulls` | PR list | `owner_profile`, `sibling_repo` |
| `/<owner>/<repo>/issues/<n>` | issue | `owner_profile`, `repo_issue`, `issue_label`, `author_other_issue` (deferred) |
| `/<owner>/<repo>/issues` | issue list | `owner_profile`, `repo_issue` |
| `/<owner>` | profile | `owned_repo`, `pinned_repo`, `sponsor_page`, `sponsored_profile`, `contribution_org` |
| `/sponsors/<login>` | sponsor | `owner_profile`, `similar_sponsor` |

Reserved top-level paths (`/settings`, `/marketplace`, `/explore`,
`/topics`, `/notifications`, `/advisories`, etc.) classify as Unknown
and probe to no candidates.

### `GistStrategy` (`github.gist`)

| URL shape | Candidates emitted |
|-----------|--------------------|
| `/<owner>/<gist_id>` | `owner_profile`, `owner_gist` (captured gist excluded by ID) |
| `/<owner>` | `owner_profile`, `owner_gist` (all of owner's gists) |

### `SecurityAdvisoryStrategy` (`github.advisory`)

| URL shape | Surface | Candidates emitted |
|-----------|---------|--------------------|
| `/advisories[/<ghsa>]` | global db | `similar_advisory` (captured GHSA excluded) |
| `/<owner>/<repo>/security/advisories[/<ghsa>]` | per-repo | `owner_profile`, `advisory_repo`, `similar_advisory` |

## Wiring

The daemon registers the family via a single call:

```go
import (
    "github.com/ideacrafterslabs/ctxt/internal/lateral"
    "github.com/ideacrafterslabs/ctxt/internal/lateral/strategies/github"
)

reg := lateral.NewRegistry()
github.Register(reg, github.DefaultConfig(), github.SharedDeps{
    APIClient:       apiClient,       // required when EnableParent=true
    Fetcher:         httpFetcher,     // optional; needed only if APIClient depends on it
    Breaker:         breaker,         // optional; wraps APIClient via NewGuardedAPI
    Floor:           floorTracker,    // optional; same wrap
    FailureRecorder: busRecorder,     // optional; installed via SetFailureRecorder
})
```

`DefaultConfig()` enables the parent + both children. Per-strategy
disabling is supported by passing a custom `github.Config`. Disabling
the parent disables the family entirely — children-only configs would
orphan plain-github captures.

`Register` panics at init when `EnableParent=true` but `APIClient` is
nil (fail-loud-at-init mirrors substrate). Other configurations return
a count of registered strategies for log/test inspection.

## Operating

### Rate-limit dynamic floor

The github family ships a `FloorTracker` (`ratelimit.go`) that maintains
a trailing-window count of API calls and computes the absolute
remaining-call floor we must keep above to reserve budget for
non-lateral traffic.

- Trailing window: configurable (default 4h).
- Floor bounds: `[5%, 90%]` of `RateSnapshot.Limit` by default.
- Pressure model: `min(1, callsInWindow / Limit)` linearly interpolates
  the floor pct between MinPct and MaxPct.

`FloorTracker.ShouldThrottle()` returns true when
`Remaining < Floor()`. Wiring a `FloorTracker` into `SharedDeps.Floor`
+ `SharedDeps.Breaker` makes every API call short-circuit with
`ErrFloorThrottled` once the budget reserve hits, without changing
strategy code.

### Circuit breaker integration

The package-local `Breaker` interface (Allow + Record) is satisfied by
`hop.top/kit/go/core/breaker.Breaker`. The daemon adapts it; the
`guardedAPI` decorator (`breaker.go`) gates every API call on
`Allow()` and feeds outcomes to `Record(success, n)` so the breaker's
state machine sees real signal.

`RateSnapshot` pass-through is intentionally **NOT** gated — observing
the rate-limit bucket must work even when other API calls are
throttled.

### Identity-key extraction

Each emitted Candidate carries the canonical github identity key in
`Preview[github.PreviewKeyIdentityKey]`. Three scopes:

- `@github.repo.<owner>/<name>` — single repository
- `@github.user.<login>` — github user account
- `@github.org.<login>` — github organization

User vs. org disambiguation: strategies emit `@github.user.*` by
default. When a `UserSummary.Type == "Organization"` is observed, the
key is promoted to `@github.org.*` via `userCandidate`.

The substrate's identity resolver consumes these keys via
`github.ExtractIdentityKey(c)` — daemon-side adapter code lifts the
preview value into `identity.Candidate.IdentityKey` before invoking
the resolver. Substrate's `lateral.Candidate` has no `IdentityKey`
field today; this preview-based contract bridges the gap until it
does.

`github.ParseIdentityKey(key)` decomposes a key into structured form
(`Kind`, `Owner`, `Name`, `Login`).

### Failure recording

Probes degrade silently on per-sub-path API errors. Errors emit
`ctxt.lateral.subpath.failed` via the package-global recorder
(`failure.go`); the daemon installs a `BusFailureRecorder` via
`SharedDeps.FailureRecorder`. Skeleton mode uses a no-op recorder.

Payload qualifiers:

- `Mechanism` — strategy ID (e.g. `github`, `github.gist`)
- `Reason` — sub-path label (e.g. `list_repo_siblings`,
  `has_sponsor_page`, `list_global_advisories`)
- `Property` — error string (when applicable)
- `Subject` — captured object ID

## Events

GitHub strategies emit only the substrate's existing topics via the
`FailureRecorder`:

- `ctxt.lateral.subpath.failed` — per-sub-path API call failure.
- `ctxt.lateral.scan.failed` — strategy-level abort (rare; reserved
  for future use).

No github-specific topics are introduced.

## Worked example — capture `https://github.com/samber/lo`

1. Capture pipeline persists the URL; substrate publishes
   `ctxt.ingest.object.persisted`.
2. lateral.discover reads the parent record, calls
   `Registry.Dispatch(event)`.
3. All three github strategies' `Applies()` return:
   - `GitHubStrategy` → `(true, 1)`
   - `GistStrategy` → `(false, 0)` (host isn't gist)
   - `SecurityAdvisoryStrategy` → `(false, 0)` (path isn't advisory)
4. Dispatcher picks the highest-specificity match → `GitHubStrategy`.
5. `GitHubStrategy.Probe()` classifies the path as `pageTypeRepo`,
   dispatches to `probeRepo`.
6. `probeRepo` fans out 5 API calls in sequence:
   - `ListRepoSiblings("samber", "lo")` → `[do, mo, ...]` siblings.
   - emit `owner_profile` candidate (`https://github.com/samber`).
   - `ListOwnerPinned("samber")` → pinned repos.
   - `HasSponsorPage("samber")` → bool.
   - `ListOwnerStarred("samber")` → starred repos.
7. Each candidate carries `Preview[identity_key]`; substrate identity
   resolver lifts via `ExtractIdentityKey` and resolves canonicals.
8. Cap gate + scoring stage filters candidates; remainder go to
   materialization.

The integration test `TestIntegration_CaptureSamberLo` exercises this
full path against recorded fixtures.

## Limits

### Today

- `ListOwnerPinned`, `ListSponsored`, `ListSimilarSponsors` return
  `nil, nil` from `HTTPAPIClient` — github has no REST surface for
  these. Daemon adapters with GraphQL access override directly.
- `author_other_pr` and `author_other_issue` candidate types are
  scaffolded behind `authorHintFor`, which always returns `""` in v1.
  When the daemon stages author hints (e.g. via active-context
  fingerprint), these candidates start emitting without strategy
  changes.
- xrr is not in the build path; cassette tests use httptest.Server +
  fixtureFetcher. The Fetcher injection point unchanged when xrr lands.

### Out of scope (deferred)

- Other github sub-paths: discussions, releases, wikis, projects.
- Repo-internal sub-paths beyond pull/issues (commits, files, blame).
- Fine-grained PAT scope detection (rate-limit floor uses unauthenticated
  + authenticated buckets uniformly).
- Multi-account auth rotation.

### Known unknowns

- Substrate `lateral.Candidate` has no `IdentityKey` field; we use
  `Preview[identity_key]` as the documented contract. When substrate
  adds the field, all callsites flip to that field; `ExtractIdentityKey`
  becomes a one-line passthrough.
- Pinned items via HTML scrape would unblock `ListOwnerPinned` without
  GraphQL; deferred until ibr lands as a Fetcher implementation.

## File map

| File | Purpose |
|------|---------|
| `strategy.go` | `GitHubStrategy` parent + `Probe` dispatcher + classifier |
| `gist.go` | `GistStrategy` child |
| `advisory.go` | `SecurityAdvisoryStrategy` child |
| `probe.go` | `probeRepo`, `probePR`, `probeIssue`, `probeProfile`, `probeSponsor` |
| `urls.go` | URL parsing + identity-key constructors |
| `api.go` | `APIClient` interface + summary types |
| `fetcher.go` | `Fetcher` interface |
| `httpclient.go` | `HTTPAPIClient` reference impl |
| `ratelimit.go` | `RateSnapshot` + `FloorTracker` |
| `breaker.go` | `Breaker` interface + `guardedAPI` decorator |
| `failure.go` | `FailureRecorder` + bus emission helpers |
| `identity.go` | `ExtractIdentityKey`, `ParseIdentityKey` |
| `wiring.go` | `Register`, `Config`, `SharedDeps` |

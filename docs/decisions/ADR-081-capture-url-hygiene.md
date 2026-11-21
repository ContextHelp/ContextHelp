# ADR-081 – Capture URL Hygiene: Canonicalization and Noise Presets for Browser Capture

> **Status:** Accepted (2026-11-21; decisions Q1-Q7 and Q9 as recommended, Q8 open; see [Decisions](#decisions))
> **Date:** 2026-11-21
> **Author:** jadb
> **Applies to:** ctxt browser capture: `ctxt capture tabs`, `ctxt capture history` and the ambient `browserhistory` source; dpkms only through the existing enqueue API
> **Extends:** ADR-066 (ambient capture substrate: privacy enforcement before the event leaves the machine)
> **References:** ADR-056 (unified enqueue API), ADR-077 (ctxt is a pure dpkms API client; local capture stays local), ADR-078 (no plugin system), ADR-070 (pipeline versioning)
> **Code today:** `internal/urlfilter` (`config.go`, `pattern.go`, `urlfilter.go`), `cmd/ctxt/cmd/capture_tabs.go`, `cmd/ctxt/cmd/capture_history.go`, `internal/ambient/browserhistory/browserhistory.go`, `internal/lateral/strategies/github/urls.go`

---

## Context

`ctxt capture tabs`, `ctxt capture history` and the ambient `browserhistory` source send each allowed browser URL to dpkms as one enqueue. All three run URLs through the same `urlfilter.Filter` (`internal/urlfilter/urlfilter.go:3-6`; ambient at `internal/ambient/browserhistory/browserhistory.go:211`). Two kinds of junk reach the knowledge base because nothing sits between "the browser has this URL" and "enqueue it":

**1. Equivalent URLs are different captures.**

- A GitHub file view (`github.com/o/r/blob/main/src/x.go#L10-L40`) is captured as its own object, one per file, branch and anchor. Reading five files of one repo yields five objects and no repo object.
- Tracking parameters (`utm_*`, `fbclid`, `gclid`, `mc_eid`, ...) make one page several URLs.
- Fragments make one document several URLs (`docs/page#install`, `docs/page#usage`).
- Dedup does not help. `capture tabs` and `capture history` dedupe only on the exact URL string within one run (the `seen` map in `evaluateTabs` / `evaluateVisit`). On the server, a URL capture has `Source` = the URL (`internal/service/service.go:310-312`), so the enqueue-time check `ContentHash(URL, URL)` (`service.go:476-478`) can only match an object stored with that same hash; stored URL objects are hashed over the fetched body plus source (`internal/jobs/worker.go:274`, body set at `internal/pipeline/steps/url_fetcher.go:132`), so the enqueue check never hits for a URL capture. The real dedup is the worker's reinforce on that `(body, source)` hash (`worker.go:290`). Two URL spellings are therefore two sources and two objects even when the page body is identical. GitHub drafts that carry a forge identity hash over that identity instead (`storageutil.DraftContentHash`, `internal/storageutil/content_hash.go:31`).

**2. Pages that should never be knowledge.**

Webmail, calendar, banking, login/SSO, checkout and internal admin consoles are open in most people's browsers. Today the only shipped filter content is `builtinDeny` in `internal/urlfilter/config.go`: four loopback entries, with the rule "only generic, never user- or organization-specific, entries belong here". Everything else is the user's `capture.url_filter` deny list, which starts empty. A first `capture history` run therefore sends a day of inbox, bank and SSO URLs, and the user must discover and write the rules themselves.

**3. Bookmark import bypasses `url_filter` (finding).**

Bookmarks are not under `capture`. `ctxt import chrome|edge|firefox|safari` (`cmd/ctxt/cmd/import_bookmarks.go`, `import_chrome.go`, `import_edge.go`, `import_firefox.go`, `import_safari.go`) parses a bookmarks HTML export and enqueues every URL through `enqueueBookmark` (`import_bookmarks.go:148`). None of the `import_*` files reference `internal/urlfilter`, so bookmarks ignore even the user's own `capture.url_filter` deny and `allow_only` rules and the builtin loopback/scheme gate. This is a gap regardless of presets. Its dry-run prints the parsed count and a preview of the first 20 URLs (`import_bookmarks.go:73`), with no per-URL status.

### What exists and must be reused

| Need | Existing piece |
|---|---|
| URL parse, scheme gate, host normalization (lowercase, trailing dot, IDN to punycode) | `internal/urlfilter/pattern.go` (`normalizeHost`, `golang.org/x/net/idna`) |
| Layered deny / `allow_only` rules, scopes global/browser/profile, merge across config layers that can only narrow | `internal/urlfilter` (`Config.For`, `Merge`) |
| Decision without the URL, safe to log | `urlfilter.Decision`, `LogValue` |
| Dry-run listing with per-URL status and reason | `tabsReport`, `visitOutcome` in the capture commands |
| GitHub URL structure (owner, repo, number, reserved top-level segments) | `parseGitHubURL`, `isReservedTopLevel` in `internal/lateral/strategies/github/urls.go` (package-private) |
| Layered config with env override and `ctxt config paths` | `internal/config` (kit-backed) |
| URL parsing | `net/url` (stdlib). No third-party URL-normalization dependency is proposed. |

Not found in the repo, so nothing to reuse: tracking-parameter lists, any canonicalization of capture URLs, any shipped category presets. No shipped preset or level system (default/strict/minimal/off) exists in the repo either; this ADR defines the levels itself. The hop.top/kit packages offer no URL normalization or category-list package. Before implementation, re-check kit for one; if kit has it, use it.

### Constraints

- ADR-066: privacy enforcement happens before the URL leaves the machine, client side.
- ADR-077: ctxt is a pure API client; local capture state stays local. Server-side selection would see the noisy URLs, which is the thing to avoid.
- Decisions are logged without the URL (`Decision` holds none). New logging must keep that.
- Layered config can only narrow capture: `Config.Merge` unions deny rules and never removes one (`internal/urlfilter/merge.go`), but it is layer-blind. Every knob this ADR adds that lowers protection must therefore be gated by config layer (see Layer rule), not by `Merge`.

---

## Decision

Two client-side stages, run in `internal/urlfilter` (new files) and called by `capture tabs`, `capture history` and the ambient `browserhistory` source, in this order:

```
raw URL -> scheme gate -> noise presets + user rules (on the RAW URL)
        -> canonicalize (allowed URLs only) -> re-evaluate rules on the canonical URL
        -> dedup on canonical form -> enqueue
```

Filter order. Path rules are prefix matches (`internal/urlfilter/pattern.go:286`, `strings.HasPrefix`), so a rule `*://intra.example/hr/` matches `/hr/x` and `/hr/` but not the canonical `/hr` that `trailing_slash` produces. Canonicalizing first would let a denied URL through. Chosen: filter the raw URL first, canonicalize only URLs that passed, then evaluate the canonical URL again and deny if it matches any rule too (raw OR canonical). Raw-first is the privacy gate, so no canonicalization can widen capture; the canonical re-check guarantees the URL actually sent is itself one no rule denies. Rejected: canonical-only (the leak above) and raw-only (the sent URL could match a rule it was not checked against, e.g. after fragment or tracking-param removal on an `allow_only` list). `Canonicalize` failing on an allowed URL denies it (fail closed, reason `canonicalize_error`); the URL is never sent raw as a fallback.

### Sources

Browser URLs enter through four commands. Today three use `urlfilter` and bookmark import does not. Treatment (the bookmark row is decided by question 9):

| Source | Command / code | Canonicalization | User `url_filter` rules | Shipped noise presets | Dry-run / report surface |
|---|---|---|---|---|---|
| Tabs | `ctxt capture tabs` (`capture_tabs.go`) | on | on (today) | on at `default` | per-tab status, reason, canonical form, counters |
| History, CLI | `ctxt capture history` (`capture_history.go`) | on | on (today) | on at `default` | per-visit outcome, canonical form, counters |
| History, ambient | `browserhistory` source (`internal/ambient/browserhistory/browserhistory.go:211`) | on | on (today) | on at `default` | no terminal output: events carry rule scope and counts, never the URL |
| Bookmarks | `ctxt import chrome\|edge\|firefox\|safari` | on | on (closes the bypass) | **off by default** (a bookmark is a deliberate save); enabled per source with `capture.url_filter.hygiene.sources.bookmarks.preset` | needs per-URL status and counters in `--dry-run` (today a count and a 20-URL preview) |

A per-source `preset` under `sources.<tabs|history|ambient|bookmarks>` follows the Layer rule: setting a source below its shipped default is a lowering knob and user-layer-only. Raising bookmarks to `default` is a raising knob and honored from any layer.

### Stage 1 – Canonicalization

Pure function `Canonicalize(raw string) (Result, error)`, `Result{URL string, Rewrites []Rewrite}`, where each `Rewrite` has a short kind (`strip_tracking`, `drop_fragment`, `github_repo`, ...) and no URL text.

Default rewrites (each individually switchable):

| Rewrite | Rule |
|---|---|
| `drop_fragment` | Remove `#...`. Exception: none by default. SPA hash-routes (`#/inbox`) are the case this loses; they are mostly covered by the noise presets. |
| `strip_tracking` | Remove query keys in a shipped list: `utm_*`, `fbclid`, `gclid`, `dclid`, `msclkid`, `mc_cid`, `mc_eid`, `igshid`, `_hsenc`, `_hsmi`, `ref_src`. Remaining params are kept in original order so distinct documents (`?id=7`) stay distinct. |
| `host_case_default_port` | Lowercase host and drop the trailing dot (reuse `normalizeHost`, `pattern.go:196`; it does not touch ports). Dropping the default port (`:80` on http, `:443` on https) is new code in `Canonicalize`. |
| `trailing_slash` | Treat `/a` and `/a/` as one (strip a single trailing slash on non-root paths). |
| `github_repo` | See open question 1. |

User knobs: `capture.url_filter.hygiene.canonicalize.strip_params` (extra keys, additive, any layer), `keep_params` (keys never stripped, for sites where a param is the document id) and `rewrites.<name>: false`. `keep_params` and `rewrites.<name>: false` lower protection (a kept tracking param can carry an identifier) and are user-layer-only, see Layer rule.

Only the canonical URL is sent. The original is not stored (privacy: a tracking param can carry an email hash). Dedup (`seen`) keys on the canonical URL of allowed URLs only, so `a#x` and `a#y` collapse to one send and count under `deduped`. On the server, canonical URLs help because `Source` is the URL and is part of the stored hash (see Context): the same canonical URL with an identical fetched body reinforces one object. A page whose body changes per fetch (timestamps, rotating ads) still will not dedup; that is out of scope.

GitHub file URLs. Decided: **keep the file, strip the line anchor (the fragment, by `drop_fragment`), and do not synthesize a repo capture.** "Ref noise" is not rewritten: the ref is a path segment (`/blob/<ref>/<path>`), and removing or rewriting it would change which file is captured. Rationale: a `blob`/`tree` URL is a deliberate read of that file; collapsing to the repo root loses it. `parseGitHubURL` (`internal/lateral/strategies/github/urls.go:33`) does parse owner and repo out of a blob URL (parts 0 and 1, then returns at `len(parts) < 4` or ignores the verb), but it has no blob/tree verb, and the `url.github.repo` pattern is `^https?://(www\.)?github\.com/[^/]+/[^/]+/?$` (`internal/pipeline/builtins/url_github_repo.go:7`), which rejects any path below the repo and any query. Kept blob/tree URLs therefore route to `url.generic` and fetch GitHub's HTML page. The same pattern means `github.com/o/r?tab=readme` also routes to `url.generic`: `tab` is not in the tracking list, so it is not stripped, and a repo URL with any query is not a repo capture. Alternatives are in the options table.

### Stage 2 – Noise presets

Shipped category presets of deny rules, expressed as ordinary `urlfilter.Rules` so they reuse the matcher, `Decision` and dry-run reasons. They form a new layer between `builtin` and `global`, with `Rule.Scope` = `preset:<level>/<category>` (for example `preset:default/webmail`). `Decision` gains no `Preset` or `Category` field: both are read from `Rule.Scope`, one source of truth.

Rule syntax is the existing one (`internal/urlfilter/pattern.go:10-23`): a bare host rule matches any scheme, port and path and is invalid with a path in it; a rule with a path or path glob uses URL form `scheme://host[/path]`. Queries are never matched. Examples below are written in valid form.

Categories (initial, each a small reviewed list of generic hosts and path patterns, no organization-specific entries). A host belongs to exactly one category:

| Preset | Examples (not exhaustive) |
|---|---|
| `webmail` | `mail.google.com`, `outlook.live.com`, `mail.yahoo.com`, `*://outlook.office.com/mail/*`, `*.protonmail.com` app host |
| `calendar` | `calendar.google.com`, `*://outlook.office.com/calendar/*` |
| `finance` | major bank and brokerage login/account hosts by region; patterns such as `*://*/accounts/*` are rejected as too broad |
| `auth` | `accounts.google.com`, `login.microsoftonline.com`, `*.okta.com`, `*.auth0.com`, `appleid.apple.com`, `*://*/login`, `*://*/signin`, `*://*/oauth/authorize` |
| `checkout` | `*://*/checkout`, `*://*/checkout/*`, `*://*/cart`, `*://*/cart/*`, `checkout.stripe.com`, `*://*.paypal.com/checkoutnow*`; bare `*/cart*` is rejected (it also hits `/cartography`) |
| `admin_consoles` | `console.aws.amazon.com`, `console.cloud.google.com`, `portal.azure.com`, `*.admin.*` |

`outlook.office.com` is split by path between `webmail` and `calendar` (the earlier draft listed the whole host in both, which made disabling `calendar` a no-op). A shipped-list test asserts no rule string appears in two categories.

Levels (defined here; no preset system exists in the repo yet):

| Level | Enabled categories |
|---|---|
| `off` | none (today's behavior) |
| `minimal` | `auth`, `checkout`, `finance` (credentials and money) |
| `default` | `minimal` + `webmail`, `calendar` |
| `strict` | `default` + `admin_consoles` + the credential-key check below |

The `strict` credential-key check (`token`, `key`, `code`, `session` as a query key) cannot be a `urlfilter.Rules` entry, because the matcher never looks at queries (`pattern.go:15-17`). It is its own check with its own `Reason` (`credential_query_key`). It logs and reports only the key kind (`credential_query_key`), never the key name or value, and never the URL. Whether it stays in `strict` is open question 8.

Config shape. This is new config under the existing `capture.url_filter` key, so presets reuse `Config.For` and `Merge` and the per-browser/per-profile scopes that already live at `capture.url_filter.browsers` (`internal/urlfilter/config.go:41`, `internal/config/config.go:272`), not a parallel `capture.url_hygiene` tree:

```yaml
capture:
  url_filter:
    deny: []                   # existing
    hygiene:
      preset: default          # off | minimal | default | strict
      categories:              # per-category override of the preset
        calendar: false
        admin_consoles: true
      custom:                  # user categories, same rule syntax as url_filter
        work_crm:
          deny: ["crm.example.net"]
      canonicalize:
        rewrites: { drop_fragment: true }
        strip_params: []
        keep_params: []
    browsers:                  # existing per-source scope
      brave:
        profiles:
          Work:
            hygiene: { preset: minimal }
```

Layer rule. Merge is layer-blind and the project layer is a file a cloned repository can supply (`.contexthelp/ctxt.yaml`, `internal/config/registry_layer.go:32-34`). Every knob that lowers protection is therefore honored only from user-owned layers: `preset:` set below the shipped level (`off`, `minimal`) at any scope including `browsers.*.profiles.*`, `categories.<x>: false`, `canonicalize.keep_params`, and `canonicalize.rewrites.<name>: false`. Knobs that only raise protection (`preset: strict`, `categories.<x>: true`, `custom` deny rules, `strip_params`) are honored from every layer. Lookup follows `RegistriesSource` (`internal/config/registry_layer.go:35`): a helper walks `layerSources` from highest precedence, skips `LayerProject`, and reads the knob with `OpenLayer(...).Has(key)`. Layer accounting: `LayerUser` counts; `LayerSystem` counts (administrator-owned); `LayerExtra` counts (a file or `-c key=value` the user named on this invocation); `LayerProject` never counts. There is no env binding for any hygiene key, so env cannot lower anything. A lowering knob found only in the project layer is ignored and the command prints one warning naming the key (not the value). Open question 3 is decided accordingly.

### Visibility and privacy

- `--dry-run` lists, per URL: status, the canonical form when it differs (shown only on the local terminal and in the local JSON report, as the URL already is today), the rewrites applied by kind, and for a deny the rule scope (`denied by deny rule "mail.google.com" (preset:default/webmail)`, the existing `Decision.String` format). JSON report fields: `url` (raw, as today) and `canonical_url` (present only when it differs); `rewrites` is a list of kinds.
- `ctxt capture tabs --explain-hygiene <url>` (optional, later): one URL through both stages.
- Logs and events carry the `Decision` fields as today: reason, list and scope, and the matching rule pattern (`Decision.LogValue`, `internal/urlfilter/urlfilter.go:100`). For shipped presets the pattern is generic and safe. A user `custom` or `deny` pattern can be organization-specific (`crm.example.net`), and it is logged as today; that is existing behavior, not new exposure. No URL and no query keys or values are logged.
- Reports gain summary counters `canonicalized`, `denied_by_preset` next to `denied` and `deduped`.
- Preset lists are versioned with the ctxt release and listed by `ctxt capture presets` (name, categories, rule count); a list change is a release-note item, since it changes what a default install captures.

### Interaction with the pipelines and dedup

- Server dedup is unchanged and still works on `(fetched body, source)` (Context). Canonicalization makes equivalent URLs the same `Source`, so identical fetched bodies reinforce one object instead of creating several; it does not make the enqueue-time `ContentHash(URL, URL)` check effective, which never matches for URL captures.
- Pipeline selection is untouched: the canonical URL goes through the existing URL detector. Anchors and tracking params no longer reach it. A repo URL with a non-tracking query (`?tab=readme`) is not stripped and routes to `url.generic` (see GitHub file URLs).
- Re-capturing a page whose stored object came from a pre-canonicalization URL creates one new object; it does not rewrite history. No migration (unreleased, no backcompat paths).

---

## Options considered

### Canonicalization location

| Option | Pros | Cons |
|---|---|---|
| **A. Client-side in `internal/urlfilter` (recommended)** | Fits ADR-066/077: URL never leaves the machine in raw form; dry-run can show the rewrite; one implementation shared by tabs, history and ambient browserhistory; no server change | Other clients (extension, mobile, `ctxt capture <url>`) do not get it unless they call the same package |
| B. Server-side pipeline step at the head of `url.*` pipelines | Every client benefits; fits the pipeline model; per-pipeline override via `pipelines.overrides` | Raw URL (with tracking ids) reaches the server and the job payload; dedup happens after enqueue; dry-run cannot show it; changes pipeline versions (ADR-070) |
| C. Both: client does it for capture commands, a thin server step re-applies idempotently | Defense in depth | Two implementations to keep equal; most complexity for least gain |

### GitHub file URLs

| Option | Pros | Cons |
|---|---|---|
| **A. Keep the file, strip the anchor (recommended default)** | Preserves intent; no surprise objects | Many file captures per repo remain |
| B. Collapse `blob`/`tree` to repo root | One repo object; matches the `github.*` repo pipeline; kills near-duplicates | Loses the file; user who wanted that file gets the README-level summary |
| C. Keep the file and also enqueue the repo root (once per run, deduped) | Both signals | Doubles sends; the repo capture was not requested; surprises the dry-run |
| D. Config switch `github_file: keep | repo | both`, default `keep` | Lets the owner decide per profile | A knob for a taste question |

### Noise filtering

| Option | Pros | Cons |
|---|---|---|
| A. Status quo: generic builtin only, user writes rules | Zero shipped opinions; nothing to maintain | First run captures inbox/bank/SSO; privacy depends on user diligence |
| B. Shipped presets, on by default (`default` level) (recommended) | Safe first run; matches ADR-066 privacy-before-send; users opt out | Shipped lists are opinions and need maintenance; false positives silently skip pages (mitigated by dry-run reason and counters) |
| C. Shipped presets, off by default, `ctxt capture` prints a hint to enable | No behavior change for existing users | The users who need it most never turn it on |
| D. Heuristic classifier (login forms, `type=password`) | Catches the long tail | Needs page content or a model; not available at URL-only stage; non-deterministic |

Recommended: A for canonicalization location, A for GitHub, B for noise (presets on at `default`, one-time notice on first run naming the level and `ctxt capture presets`).

---

## Consequences

Positive: fewer, cleaner objects; reinforcement across URL spellings; safer default capture; one place (`internal/urlfilter`) holds URL policy; dry-run explains every change.

Negative: shipped category lists must be curated and tested (table-driven tests per category, plus a "no organization-specific entries" review rule as for `builtinDeny`); default behavior changes what a fresh install captures; canonical-only sending drops the original URL (fragment, tracking ids).

Operator impact (ADR-070 declaration, when implemented): behavior change for clients, none for the dpkms daemon (no schema, API or pipeline change).

## Decisions

The owner accepted the recommendation stated for each of questions 1-7 and 9. The ADR is Accepted with question 8 carved out as open, as ADR-066 and ADR-080 are; nothing is implemented. The review amendments (filter-before-canonicalize, layer rule, config shape) are part of the accepted design.

| # | Question | Decision |
|---|---|---|
| 1 | GitHub `blob`/`tree` URLs | Keep the file, strip the anchor (option A). No ref rewriting, no repo-root collapse, no extra repo enqueue, no per-profile switch. |
| 2 | Default preset level | `default` on for everyone, with a one-time notice on first run naming the level and `ctxt capture presets`. The "shown" flag is a marker file under the ctxt state directory (XDG state, via the existing `internal/config` path helpers), never in a config layer, so it cannot be set by a project file. |
| 3 | Who may disable a shipped category | User-owned layers only, extended to every lowering knob (level below shipped, category off, `keep_params`, `rewrites.<name>: false`); see Layer rule. |
| 4 | Fragments | Drop all fragments. Presets already skip the main hash-routed apps (webmail, calendar). |
| 5 | Scope of canonicalization (capture sources) | Browser capture only: `capture tabs`, `capture history` and the ambient `browserhistory` source (same filter, same flow). Not `ctxt capture <url>` or `ctxt analyze <url>`: a pasted URL may be wanted verbatim. Bookmark import is not covered by this decision; see question 9. |
| 6 | Tracking-param and category list ownership | Release-bound lists maintained in ctxt; no network fetch, no registry-delivered updates. A list change is a release-note item. |
| 7 | `parseGitHubURL` sharing | Export from a small `internal/githuburl` package so canonicalization and the github strategies agree on URL shape; no duplication. |
| 9 | Bookmark import | In scope: canonicalization and the user `url_filter` deny/`allow_only` rules apply to `ctxt import chrome\|edge\|firefox\|safari`, closing today's bypass. Shipped noise presets default off for bookmarks; `capture.url_filter.hygiene.sources.bookmarks.preset` raises them. Bookmark `--dry-run` reports per-URL status and counters like the other sources. |

## Open questions for the owner

8. Strict-level credential-key check (`token`, `key`, `code`, `session` in the query): acceptable false-positive rate, or drop it from `strict`? No recommendation is stated; decide before implementing `strict`. Other levels do not depend on it.

## Test plan

- Golden table for `Canonicalize`: input, canonical output, rewrite kinds; includes tracking params, fragments, trailing slash, default ports, IDN and case hosts, `?id=7` preserved, `keep_params`.
- Idempotence fuzz: `Canonicalize(Canonicalize(u)) == Canonicalize(u)` over generated URLs.
- Deny property test: for every shipped and generated rule and URL `u`, if `Evaluate(u)` denies, no canonical or enqueued URL is produced; and any URL sent satisfies `Evaluate(canonical).Allowed`. Includes the `/hr/` vs `/hr` case.
- Per-category tables: each category has allowed and denied URLs, no rule string in two categories, no organization-specific entries, `/cartography` allowed.
- Layer tests: a project-layer file setting `preset: off`, `categories.x: false`, `keep_params` or `rewrites.<n>: false` is ignored with a warning; the same keys in user, system and extra layers apply; a project-layer `strict` or `custom` deny applies.
- Canonicalize error on an allowed URL denies it and sends nothing.
- All callers (`capture tabs`, `capture history`, ambient `browserhistory`, and bookmark import) call the same pipeline; a table shared by their tests. Bookmark import test: a URL denied by a user rule is not enqueued, and presets stay off unless configured.
- Logging test: no URL, query key or query value in any log line or event payload.
- Everything is pure `net/url` and string work, with no OS-specific code, so one test set covers macOS, Linux and Windows; CI runs it on all three.

# ADR-081 – Capture URL Hygiene: Canonicalization and Noise Presets for Browser Capture

> **Status:** Proposed (2026-11-21)
> **Date:** 2026-11-21
> **Author:** jadb
> **Applies to:** ctxt (`ctxt capture tabs`, `ctxt capture history`); dpkms only through the existing enqueue API
> **Extends:** ADR-066 (ambient capture substrate: privacy enforcement before the event leaves the machine)
> **References:** ADR-056 (unified enqueue API), ADR-077 (ctxt is a pure dpkms API client; local capture stays local), ADR-078 (no plugin system), ADR-070 (pipeline versioning)
> **Code today:** `internal/urlfilter` (`config.go`, `pattern.go`, `urlfilter.go`), `cmd/ctxt/cmd/capture_tabs.go`, `cmd/ctxt/cmd/capture_history.go`, `internal/lateral/strategies/github/urls.go`

---

## Context

`ctxt capture tabs` and `ctxt capture history` send each allowed browser URL to dpkms as one enqueue. Two kinds of junk reach the knowledge base because nothing sits between "the browser has this URL" and "enqueue it":

**1. Equivalent URLs are different captures.**

- A GitHub file view (`github.com/o/r/blob/main/src/x.go#L10-L40`) is captured as its own object, one per file, branch and anchor. Reading five files of one repo yields five objects and no repo object.
- Tracking parameters (`utm_*`, `fbclid`, `gclid`, `mc_eid`, ...) make one page several URLs.
- Fragments make one document several URLs (`docs/page#install`, `docs/page#usage`).
- Dedup does not help. `capture tabs` and `capture history` dedupe only on the exact URL string within one run (the `seen` map in `evaluateTabs` / `evaluateVisit`). The server dedupes on `ContentHash(content, source)`, and for a URL capture the content is the URL, so any textual difference is a new object.

**2. Pages that should never be knowledge.**

Webmail, calendar, banking, login/SSO, checkout and internal admin consoles are open in most people's browsers. Today the only shipped filter content is `builtinDeny` in `internal/urlfilter/config.go`: four loopback entries, with the rule "only generic, never user- or organization-specific, entries belong here". Everything else is the user's `capture.url_filter` deny list, which starts empty. A first `capture history` run therefore sends a day of inbox, bank and SSO URLs, and the user must discover and write the rules themselves.

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

Not found in the repo, so nothing to reuse: tracking-parameter lists, any canonicalization of capture URLs, any shipped category presets. A grep for a shipped redaction preset system (default/strict/minimal/off) finds none either; it is a planned shape in the project notes, not code. This ADR names it as the model and does not depend on it existing. Checked in hop.top/kit packages inventory: no URL normalization or category-list package. Before implementation, re-check kit for one; if kit has it, use it.

### Constraints

- ADR-066: privacy enforcement happens before the URL leaves the machine, client side.
- ADR-077: ctxt is a pure API client; local capture state stays local. Server-side selection would see the noisy URLs, which is the thing to avoid.
- Decisions are logged without the URL (`Decision` holds none). New logging must keep that.
- Layered config can only narrow capture (no layer removes a deny rule). Presets must not break that property.

---

## Decision (proposed)

Two independent client-side stages, run in `internal/urlfilter` (new files) and called by both capture commands, in this order:

```
raw URL -> scheme gate -> canonicalize -> noise presets + user rules -> dedup on canonical form -> enqueue
```

### Stage 1 – Canonicalization

Pure function `Canonicalize(raw string) (Result, error)`, `Result{URL string, Rewrites []Rewrite}`, where each `Rewrite` has a short kind (`strip_tracking`, `drop_fragment`, `github_repo`, ...) and no URL text.

Default rewrites (each individually switchable):

| Rewrite | Rule |
|---|---|
| `drop_fragment` | Remove `#...`. Exception: none by default. SPA hash-routes (`#/inbox`) are the case this loses; they are mostly covered by the noise presets. |
| `strip_tracking` | Remove query keys in a shipped list: `utm_*`, `fbclid`, `gclid`, `dclid`, `msclkid`, `mc_cid`, `mc_eid`, `igshid`, `_hsenc`, `_hsmi`, `ref_src`. Remaining params are kept in original order so distinct documents (`?id=7`) stay distinct. |
| `host_case_default_port` | Lowercase host, drop trailing dot and default port (reuse `normalizeHost`). |
| `trailing_slash` | Treat `/a` and `/a/` as one (strip a single trailing slash on non-root paths). |
| `github_repo` | See open question 1. |

User knobs: `capture.url_hygiene.canonicalize.strip_params` (extra keys, additive), `keep_params` (keys never stripped, for sites where a param is the document id), `rewrites.<name>: false`.

Only the canonical URL is sent. The original is not stored (privacy: a tracking param can carry an email hash). Dedup (`seen`) keys on the canonical URL, so `a#x` and `a#y` collapse to one send and count under `deduped`.

GitHub file URLs. Recommended default: **keep the file, but strip the line anchor and ref noise, and do not synthesize a repo capture.** Rationale: a `blob`/`tree` URL is a deliberate read of that file; collapsing to the repo root loses it, and the `github.*` pipelines key on the URL shape (repo, PR, issue). `/blob/<ref>/<path>` and `/tree/<ref>/<path>` are not handled by `parseGitHubURL` today, so they fall to `url.generic`. Alternatives are in the options table.

### Stage 2 – Noise presets

Shipped category presets of deny rules, expressed as ordinary `urlfilter.Rules` so they reuse the matcher, scopes, `Decision` and dry-run reasons. A new scope `preset:<name>` sits between `builtin` and `global`.

Categories (initial, each a small reviewed list of generic hosts and path patterns, no organization-specific entries):

| Preset | Examples (not exhaustive) |
|---|---|
| `webmail` | `mail.google.com`, `outlook.live.com`, `outlook.office.com`, `mail.yahoo.com`, `*.protonmail.com` app host |
| `calendar` | `calendar.google.com`, `outlook.office.com/calendar` |
| `finance` | major bank and brokerage login/account hosts by region; patterns such as `*://*/accounts/*` are rejected as too broad |
| `auth` | `accounts.google.com`, `login.microsoftonline.com`, `*.okta.com`, `*.auth0.com`, `appleid.apple.com`, path patterns `/login`, `/signin`, `/oauth/authorize` |
| `checkout` | `*/checkout*`, `*/cart*`, `pay.*`, `checkout.stripe.com`, `paypal.com/checkoutnow` |
| `admin_consoles` | `console.aws.amazon.com`, `console.cloud.google.com`, `portal.azure.com`, `*.admin.*` |

Levels, modeled on the redaction presets the project already intends (`default` / `strict` / `minimal` / `off`, with per-source enable, disable and custom):

| Level | Enabled categories |
|---|---|
| `off` | none (today's behavior) |
| `minimal` | `auth`, `checkout`, `finance` (credentials and money) |
| `default` | `minimal` + `webmail`, `calendar` |
| `strict` | `default` + `admin_consoles` + any URL with a query containing a credential-looking key (`token`, `key`, `code`, `session`) |

Config shape under the existing key family:

```yaml
capture:
  url_hygiene:
    preset: default            # off | minimal | default | strict
    categories:                # per-category override of the preset
      calendar: false
      admin_consoles: true
    custom:                    # user categories, same rule syntax as url_filter
      work_crm:
        deny: ["crm.example.net"]
    canonicalize:
      rewrites: { drop_fragment: true }
      strip_params: []
      keep_params: []
  browsers:                    # per-source scope, same shape as url_filter
    brave:
      profiles:
        Work: { preset: minimal }
```

Narrowing rule preserved: a layer may enable a category or raise the level, and may disable a category only in a scope the user owns (their user config), not through a project `.contexthelp/ctxt.yaml`. This keeps the "no layer can drop another layer's deny rule" property for shipped presets by treating `categories.<x>: false` as a user-scope-only key. Open question 3.

### Visibility and privacy

- `--dry-run` lists, per URL: status, the canonical form when it differs (shown only on the local terminal and in the local JSON report, as the URL already is today), the rewrites applied by kind, and for a deny the preset and category (`denied by preset default/webmail rule "mail.google.com"`). `Decision` gains `Preset` and `Category` strings, no URL.
- `ctxt capture tabs --explain-hygiene <url>` (optional, later): one URL through both stages.
- Logs and events carry kind, preset, category and counts only. No URL, no query keys.
- Reports gain summary counters `canonicalized`, `denied_by_preset` next to `denied` and `deduped`.
- Preset lists are versioned with the ctxt release and listed by `ctxt capture presets` (name, categories, rule count); a list change is a release-note item, since it changes what a default install captures.

### Interaction with the pipelines and dedup

- Server dedup is unchanged. Canonicalization makes its `ContentHash(content, source)` effective for URLs, because equivalent URLs now produce the same content.
- Pipeline selection is untouched: the canonical URL still goes through the existing URL detector, so `github.*` routing sees cleaner input (no `?tab=`, no anchors).
- Re-capturing a page whose stored object came from a pre-canonicalization URL creates one new object; it does not rewrite history. No migration (unreleased, no backcompat paths).

---

## Options considered

### Canonicalization location

| Option | Pros | Cons |
|---|---|---|
| **A. Client-side in `internal/urlfilter` (recommended)** | Fits ADR-066/077: URL never leaves the machine in raw form; dry-run can show the rewrite; one implementation shared by tabs and history; no server change | Other clients (extension, mobile, `ctxt capture <url>`) do not get it unless they call the same package |
| B. Server-side pipeline step at the head of `url.*` pipelines | Every client benefits; fits the pipeline model; per-pipeline override via `pipelines.overrides` | Raw URL (with tracking ids) reaches the server and the job payload; dedup happens after enqueue; dry-run cannot show it; changes pipeline versions (ADR-070) |
| C. Both: client does it for capture commands, a thin server step re-applies idempotently | Defense in depth | Two implementations to keep equal; most complexity for least gain |

### GitHub file URLs

| Option | Pros | Cons |
|---|---|---|
| **A. Keep the file, strip anchor/ref noise (recommended default)** | Preserves intent; no surprise objects | Many file captures per repo remain |
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

Positive: fewer, cleaner objects; effective server dedup; safer default capture; one place (`internal/urlfilter`) holds URL policy; dry-run explains every change.

Negative: shipped category lists must be curated and tested (table-driven tests per category, plus a "no organization-specific entries" review rule as for `builtinDeny`); default behavior changes what a fresh install captures; canonical-only sending drops the original URL (fragment, tracking ids).

Operator impact (ADR-070 declaration, when implemented): behavior change for clients, none for the dpkms daemon (no schema, API or pipeline change).

## Open questions for the owner

1. GitHub `blob`/`tree`: keep file (A), collapse to repo (B), both (C), or a per-profile switch (D)? Default recommendation: A.
2. Default level: `default` on for everyone (recommended), or `minimal`, or off with a hint?
3. May a project config disable a shipped category, or only the user's own config? (Recommended: user config only, to keep "layers only narrow".)
4. Drop all fragments, or keep them for known hash-routed apps? (Recommended: drop; presets already skip the main hash-routed apps, webmail and calendar.)
5. Should canonicalization also apply to `ctxt capture <url>` and `ctxt analyze <url>`, not just browser capture? That touches explicit user intent: a pasted URL may be wanted verbatim. Recommended: no, browser capture only.
6. Who owns the tracking-parameter and category lists, and what is the update cadence (release-bound, or fetched from a registry per ADR-034)? Recommended: release-bound, no network fetch.
7. Does `parseGitHubURL` move to a shared package so canonicalization and the github strategies agree on URL shape? Recommended: yes, export from a small `internal/githuburl` package rather than duplicating.
8. Strict-level credential-key heuristic: acceptable false-positive rate, or drop it from `strict`?

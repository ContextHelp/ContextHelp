# ADR-083 – Steward Access for Business Instances

> **Status:** Accepted (2026-10-16); the steward, approval and elevation design is not implemented; the prerequisites it builds on are (per-object grant render, the owner-only credential, invite and entity-gate rules, the sharing changelog, the administrator leak fixtures; see Context). The owner decided §1–§10 on 2026-10-16, including the review choices recorded in §2–§4, §7 and §9; the open questions stay open.
> **Date:** 2026-10-16
> **Author:** jadb
> **Applies to:** dPKMS, ctxt
> **Amends:** ADR-079 (§5 the `admin` bundle excludes the steward scopes and `approve:shares`; registry routes deliver another principal's credentials only to the owner or that principal), ADR-080 (§2 viewer inputs, §9 audit durability and retention for steward reads, §11 widening changes by a non-owner admin are pending), ADR-082 (§10 "No data-admin tier": the steward flow it deferred, and the registry rules that make administering not data access)
> **Depends on:** ADR-079 (principals, scopes, role bundles), ADR-080 (share policies, scoped driver, render layer, backstop, read audit), ADR-082 (enterprise IdP mappings, PACT admission, enrollment)
> **References:** ADR-064 (federation), ADR-075 (hooks and events), ADR-077 (ctxt is a pure API client)
> **Plan:** [`docs/plans/2026-10-14-auth-and-sharing.md`](../plans/2026-10-14-auth-and-sharing.md)

---

## Context

### The decision already taken

ADR-082 §10 records an owner decision of 2026-10-16: **there is no data-admin tier.** Administering an instance is not access to its data. Access beyond a principal's own grants is never a role. It is an explicit, audited, time-boxed grant to a named steward, issued through the existing audience and share-policy machinery, with a recorded justification and approver. The local owner stays the only bypass. Business instances (principals and roles from an external IdP, Microsoft Entra first) wait on this design: a company that owns an instance needs a lawful way to open data for compliance, eDiscovery or legal hold without making any role a skeleton key. This ADR designs the flow. It does not reopen the decision.

### What the code does today

Checked against `next` `ebaf66fb` (2026-11-21), with the per-object render and the admin-is-not-data-access rules applied (the per-object merge row and "Two holes" were re-read then; the other rows were checked against `f62e9d7b`). These are the facts this design builds on or must change. The steward, approval and elevation flows are future work: none of that code exists in the tree.

| Fact | Where | Consequence |
|---|---|---|
| The owner is the only bypass: `Resolve` returns `Unrestricted` for kind `owner` and nothing else. A role never decides a viewer, so an IdP `admin` is already a restricted viewer | `internal/sharing/resolver.go` | The negative rule of ADR-082 holds in code. Steward access has to be a new viewer input, not a role. |
| A viewer is resolved on every request (HTTP `attachCaller`, gRPC per unary call and per stream open). Grants are re-read from the store each time; the cache only memoises `Compile` by a content key (principal, ceiling, key policy, grant IDs at policy versions). An expired or revoked grant drops out of the next request's key | `resolver.go`; `compile.go`; `middleware_auth.go`; `grpc/auth.go` | Revocation and expiry are already "next request" for anything that resolves a viewer. No invalidation hook exists or is needed. |
| The scoped driver and the render layer are built per request from the viewer in the context (`sharing.Scoped`); neither caches across requests | `scoped.go` | Nothing to invalidate there. A new cache keyed on anything but the viewer's `PolicyKey` would break this. |
| Grants merge **per object**: after a scoped read the render layer asks the database which of the viewer's grants cover each released row (`ObjectStore.MatchBySQL`, each grant's compiled predicate) and merges fields and masks over those grants only; `GrantPlan.Data` is no longer the only record of which grant matched. A surface a capability opens (`entities`, `export`, `mcp`, `events`, `search: rsql`/`graph`) answers only objects a grant holding that capability covers, and renders each through every grant that covers it (ADR-080 §2) | `sharing/attribution.go`; `compile.go`; `render.go`; `scoped.go` | The plan-wide merge, a prerequisite of this design, is fixed. A grant that shows more fields or opens `export` no longer widens the rendering of objects only another grant matched, so a steward grant can be merged per object. One extra attribution query per page for a multi-grant viewer. |
| The `admin` bundle is "every scope but session-only ones" (`keep = true`); `reader` and `writer` are the `read:*` and `write:*` verbs | `internal/auth/scope.go` | Any new scope joins `admin`, and so the owner and every IdP admin, unless excluded by name. A `read:`-verb scope would join `reader` too. |
| A browser session's scopes are a fixed list intersected with the principal's | `scope.go` | A steward UI needs its scopes added there. |
| Person-principal API keys are not refused on `next`. ADR-082 §11 refuses them only where PACT enforcement is on, and that is not built (`pact.default` is `off` when unset) | ADR-082 §11 | A person may reach the API by key or by browser session. Where PACT enforcement is on, the key path closes and the CLI path for stewards waits on enrollment. |
| Non-owner reads are recorded by `AuditWriter.Record` into a bounded in-memory buffer (batch 64, 2 s, capacity 10 000). A full buffer drops the entry; a failed flush drops the batch; a crash loses the buffer. The entry is recorded by the visibility middleware, not durably before release. (Receipt-shaped entries, ADR-080 §9's amendment, are not on `next`; a receipts change, `feat/audit-receipts`, is pending.) | `internal/sharing/audit.go`; `internal/server/http/middleware_visibility.go` | Acceptable for ADR-080's "who saw Sarah". Not acceptable as the only record of a steward read: the data can be released with no record. |
| `housekeeping` prunes the object-ID lists of **every** access entry older than `sharing.audit.retention` (default 90 days) | `cmd/dpkms/cmd/housekeeping.go`; `storage/sqlite/sharing.go`; `config/sharing.go` | Steward entries need an exemption, or "who saw object X" disappears after 90 days. |
| `GET /events` is open to every viewer since ADR-080's 2026-11-20 amendment: object events are filtered per event, and the stream re-authenticates and re-resolves the viewer on each event and heartbeat. The `events` capability flag no longer closes the route. `JobService.WatchJob` is still owner-only | `http/capabilities.go` (`routeEvents`); `grpc/capabilities.go` | Forcing `events` off in a steward scope no longer keeps an elevated session off the stream. Owner decision: an elevated session is refused at stream open, by a route-level check (§4), not by the capability flag. |
| `Redact` is accepted and stored but inert; `PolicyFields` has no `topic`; `mention`/`related` are refused in a policy | `policy.go` | A steward scope cannot rely on masking and cannot select "everything that mentions person X". |

### Two holes in "administering is not data access"

Checked against `next` `ebaf66fb` with the admin-is-not-data-access change applied. The administrator leak fixtures (`internal/sharing/leaktest/admin_*`, `handlers_admin_boundary_test.go`) and an `audit_log` changelog for every sharing change (`internal/server/http/changelog.go`, `share.*` events, invite redemption included) now exist. An administrator's viewer sees nothing, as ADR-082 §10 requires. The registry routes around the viewer held the same line only in part: **impersonation is closed by an interim refusal, self-grant is still open.** The interim refusal is a 403 `CREDENTIAL_FOR_OTHER_PRINCIPAL` for any caller but the owner (a key, a sign-in link, an invite, an identity for another principal); §10 A (a 202 and a request on the shared approval mechanism) is still pending and replaces it. §10 B (widening share changes wait for approval) is not built, so the self-grant sequence below still works for a non-owner admin; the changelog records it after the fact. The table keeps the original findings:

| Fact | Where | Consequence |
|---|---|---|
| `AddMember` takes any active principal but the owner, the caller included; `CreateAudience`, `CreatePolicy` and `CreateGrant` have no check beyond `admin:shares`. A browser session holds `admin:shares` (`UISessionScopes`) | `handlers_share.go`; `handlers_share_policies.go`; `internal/auth/scope.go` | **Self-grant (still open; §10 B pending).** A non-owner admin creates an audience, adds itself, creates a policy that matches everything and grants it: four calls, no approval, then it reads the owner's objects. Adding a sibling principal it controls, or minting an invite into the audience, does the same at one remove. The audit entry is after the fact. |
| `CreatePrincipalKey` answers the key secret; `mintLink` answers the sign-in URL with the secret in its fragment; `AddIdentity` binds a proxy login or OIDC subject the caller names; `mintInvite` answers the invite URL. Only the owner guard and the scope ceiling stand in the way | `handlers_principals.go`; `ui_ceremony.go` | **Impersonation (closed by the interim 403; §10 A pending).** A non-owner holder of `admin:principals` takes a working credential for any non-owner principal, or binds its own login to one, and reads as that principal. |
| No browser session holds `admin:principals` (`UISessionScopes` lists `admin:shares` only). On `next` there is no person-key hold, so `mintKey` is not restricted by target kind and the key route reaches person targets as well as device and peer ones | `scope.go`; `handlers_principals.go` | The impersonation routes are open to every holder of `admin:principals` by key, person targets included; the link and identity routes reach persons too. ADR-082 §11 would close the person-key path only where PACT enforcement is on, so the rule cannot rest on it. |
| gRPC has no registry, sharing or credential methods: `methodScopes` lists the analyze, job, query and entity services and reflection | `internal/server/grpc/scope.go` | Nothing to change on gRPC today; the rules of §10 bind any registry method added later. |

### Constraints

- **No data-admin tier, owner bypass only** (owner-decided; above).
- **Reuse ADR-080 compilation.** No second visibility language, no second predicate path.
- **Fail closed.** Unknown, unavailable or expired is never a pass.
- **ctxt is unreleased.** No compatibility shims; SQLite and Postgres migrations in lockstep.
- **Honest scope.** dpkms controls what its own API releases. It does not control what a steward does with data once released.

---

## Decision

**dpkms adds steward grants: a record that gives one named principal a frozen share-policy scope for a bounded window, after approval by someone other than the requester. A grant is used by explicit elevation, one grant at a time. While elevated, the steward's viewer is exactly that grant's compiled scope, not a merge with the principal's ordinary view. Every elevated request leaves a durable, grant-attributed record before any data is released. Two new scopes, `request:steward` and `approve:steward`, belong to dedicated role bundles that `admin` does not contain. Expiry and revocation are computed from timestamps on every request and fail closed.**

**Approval is one shared mechanism (owner-decided 2026-10-16), built before either of its consumers: an approval request with a requester, a frozen payload, a reason, `N` distinct approvers who are never the requester, a pending TTL, a computed state machine and a full audit trail (§9). A steward grant request is one kind of request on it; a widening sharing change by a non-owner administrator is another (§10).**

**Administering is not data access, in the API as well as in the viewer (owner-decided 2026-10-16, §10): an administrator never receives a working credential for another principal, and a non-owner administrator's sharing change that could widen anyone's view stays pending until the owner or another approver accepts it. Narrowing changes and revocations apply at once; owner changes are never pending.**

### 1. Roles and who holds them

Two scopes, two role bundles. Verbs `request` and `approve` keep them out of `reader` and `writer` (`Verb()` is the part before the colon).

| Bundle | Scope | Lets the holder |
|---|---|---|
| `steward-requester` | `request:steward` | File a request for a named steward (itself or another principal); withdraw its own pending request; elevate into a grant it is the steward of. |
| `steward-approver` | `approve:steward` | List pending requests with their frozen scope and reason; approve or reject; revoke any grant; read any grant's activity report and export manifest. |

**Who may hold each.**

- **Requester.** A `person` principal, assigned the bundle locally or through an issuer-scoped IdP role mapping (ADR-082 §10), for example Entra app role `dpkms.StewardRequester` on a Legal or Compliance group. Never `device`, `peer` or an agent. The owner may not request: the owner bypasses and a grant would add nothing but misleading audit (refused `owner_cannot_request`). The owner holds `request:steward` solely so the ceiling rule lets it assign `steward-requester`; holding the scope does not let it file, and the refusal stands.
- **Approver.** A `person` principal through the same two routes. The local owner is an approver (it holds every steward scope explicitly, see below). Approvers need no data access of their own; they approve a scope, not a read.
- **Separation per grant.** The approver of a grant is neither its `requested_by` nor its `steward`. Enforced in the decision write, not in the UI. The same person may request grant A and approve grant B.
- **Two-person rule.** Instance setting `approvals.steward.required`, default `1`, `N>=1`, applied by the shared mechanism (§9): a request becomes `approved` only with `N` approvals by `N` distinct eligible approvers; one rejection by any eligible approver rejects it. `N` is frozen onto the request when it is filed, so lowering the setting never weakens a pending request. Recommended `2` for business instances.

**Relationship to `admin`.** `admin:principals` and `admin:shares` (ADR-079 §5) stay as scopes (§10 changes what some of their routes answer a non-owner), and **`admin` implies neither steward scope and neither steward bundle.** Mechanically `Bundle(admin)` excludes a named list of approval scopes (the steward scopes and §10's `approve:shares`), as `sessionOnlyScopes` is excluded today. The owner holds `admin` plus `steward-approver` and `share-approver` explicitly (`LocalPrincipal` roles). Three consequences follow from rules that already exist:

1. ADR-079 §5 "no route yields more than the caller holds" applies to assigning a bundle and to authoring an IdP role mapping. An `admin` cannot hold `approve:steward`, so **only the owner can assign a steward bundle or write a mapping that yields one.** Changing who may request or approve is owner-only and goes through `audit_log`.
2. An IdP admin cannot approve a request for themselves or for their friend by being admin; they cannot approve at all unless the owner gave them the bundle.
3. The invariant is a test: for every role bundle and every IdP mapping fixture, the effective scope set of `admin` has an empty intersection with `{request:steward, approve:steward, approve:shares}` (the last is §10's approver scope, excluded the same way).

Options considered for the bundle design.

| Option | Verdict |
|---|---|
| Steward scopes inside `admin` | Rejected. It is the data-admin tier under another name; one IdP group mapping would grant it. |
| A "steward" principal kind | Rejected. Same reasons ADR-079 rejected an `agent` kind: it duplicates what a role plus a grant express, and a kind cannot be time-boxed. |
| **Two scopes in two bundles outside `admin` (chosen)** | Reuses roles, the IdP mapping path and the ceiling rule. Requester and approver stay separable. |

### 2. The grant

A steward grant is a request on the shared approval mechanism (§9, kind `steward.grant`) and, once approved, a grant row with a frozen scope and its own lifecycle. It is not an audience membership and it is not an ordinary `share_grants` row (§3 says why). The request carries who asked, why and who approved; the grant carries what may be read and until when.

**Fields.** On the request (§9): `requested_by` (holds `request:steward`, may equal the steward), `reason`, `approvals_required` (`N`, frozen, §1), the pending TTL and the decisions; the payload is the steward, the matter reference, the scope and the window below, frozen and digest-bound. On the grant row:

| Field | Rule |
|---|---|
| `id` | `stg_<ULID>`. |
| `request_id` | The approved request it came from; one grant per request. |
| `steward_id` | The principal who will read. Must be a `person` that holds `steward-requester`. |
| `reason` (on the request) | Required for this kind, verbatim, minimum length by `steward.min_reason_chars` (proposed 30). Shown to requester, approvers, owner; included in manifests. |
| `matter_ref` | Optional external reference (case or ticket number), free text. |
| `scope` | A list of share-policy specs (ADR-080 §2) plus `include_classes`, each spec validated by the existing policy parser, **frozen** at request time (§3). |
| `scope_digest` | SHA-256 of the canonical scope, the whole list. It is part of the request's payload digest, to which each approval binds. |
| `not_before`, `expires_at` | Window. `expires_at - not_before` may not exceed `steward.max_duration` (proposed 14 days), checked when filed, when approved and **on every use**: effective expiry is `min(expires_at, not_before + max_duration)`, so lowering the cap shortens live grants. |
| `status` | Stored: `approved`, `revoked`. |
| `approved_at`, `revoked_at`, `revoked_by`, `revoke_reason`, `renews_id` | Bookkeeping. A renewal is a new request that points at its predecessor and is approved again. |

**Scope.** A list of policy specs, each the same spec as a share policy (`data`: RSQL over `PolicyFields` and `metadata.<key>`, the entity selectors v1 can express; `exclude_classes`; `fields`; `capabilities`), plus `include_classes`, with which a grant asks for a withheld class (§3). An explicit scope is filed as one spec, `include_classes` beside its keys, and freezes as a list of one. An explicit scope's `redact` is refused: masking is inert (§7), and an approver must not read as masking what the grant does not apply. One shorthand: `as_audience: <name>` freezes, at request time, the policies currently granted to that audience ("what partners could see"), each as a spec of its own, never collapsed into one. Each spec compiles to a grant of its own within the steward grant (§3). With the per-object grant merge (a prerequisite, built; see §3), the elevated viewer attributes each object to the specs covering it as a member's viewer attributes it to the grants covering it: per object, the same rows, fields and surfaces as a member. The freeze is the point: later edits to a named policy, or to the audience's grants, never widen an approved grant. **Reuse is by compilation, not by reference**: `sharing.Policy` and `Compile` take the frozen specs; no new language, no new predicate path. A steward scope never grants `mcp` or `events` in v1 (forced off on every spec; the `events` flag no longer closes `GET /events`, so streams are refused to elevated sessions by a route-level check, §4); `export` is allowed only where `steward.allow_export` is on and then only through the compliance export (§6). An explicit scope that names them (`export` where it is not allowed) is refused. `as_audience` drops them from the audience's policies instead and records what it dropped in the payload as `dropped_capabilities`, so the approvers see it. `as_audience` carries each policy's `redact` into its spec as the members have it: inert until masking lands, then applied to the steward exactly as to the members, so the elevated view never shows more than a member sees. When a frozen spec carries a `redact`, the payload records `masking_inert: true`, so the approvers see that masking is requested but not applied.

**Settings.**

| Setting | Default | Meaning |
|---|---|---|
| `steward.enabled` | `false` | Off: no routes, no scopes assignable, no elevation, no `steward.grant` requests. |
| `approvals.steward.required` | `1` | `N` above (shared mechanism, §9). |
| `steward.max_duration` | 14 days (proposed) | Hard cap. |
| `approvals.pending_ttl` | 72 h (proposed) | Shared by every kind (§9): requests not fully approved by then become `expired`. |
| `steward.elevation_idle` | 15 min (proposed) | Elevation ends after this idle time; the steward re-elevates. |
| `steward.recheck` | 30 s (proposed) | Longest interval between viewer re-resolutions on a stream or export (§4). |
| `steward.withheld_classes` | `[personal]` (proposed) | Classes removed from every steward scope (§3). |
| `steward.allow_withheld` | `false` | Whether a grant may name a withheld class in `include_classes`. |
| `steward.allow_export` | `false` | Whether a scope may open the compliance export. |
| `steward.audit_retention` | `0` (keep) | Retention of steward audit entries and their object lists; never governed by `sharing.audit.retention`. |

**States.** The request half (`requested`, `approved`, `rejected`, `withdrawn`, `expired`) is the shared mechanism's (§9). Filing a `steward.grant` request needs a holder of `request:steward`, a valid scope, a reason, a window within the cap, an eligible steward and a requester that is not the owner; the eligible approvers are §1's. On approval the grant row is written in the same transaction. The grant half below is the grant's own. `active` and `expired` are **computed** from the stored `status` and the clock on every use, as `ShareGrant.ActiveAt` does today; there is no sweeper whose lateness could extend a grant.

| From | To | Who triggers | Condition |
|---|---|---|---|
| (request approved) | `approved` | Shared mechanism | The `N`-th distinct approval of the request, each bound to its payload digest, before its pending TTL (§9). |
| `approved` | `active` | Clock | `not_before <= now < effective expiry`. |
| `approved`/`active` | `expired` | Clock | `now >= effective expiry`. Computed, so not terminal: the effective expiry follows today's cap, and raising `steward.max_duration` brings back a grant the cap had expired. |
| `approved`/`active`/`expired` | `revoked` | Any holder of `approve:steward`, the owner, `requested_by`, or the steward | Stored `status = approved`, whatever the clock or the cap says: revocation is the only end an operator can force. Effective on the next request. Terminal. |

There is no edit and no reactivation by anyone: a scope or window change is a new request. Every transition writes `audit_log` and a security event, and an approval, rejection or revocation records actor, time and note.

**Storage sketch** (instance DB, SQLite and Postgres in lockstep; non-binding). The request tables are §9's; the steward-specific rows are:

```sql
CREATE TABLE steward_grants (
  id TEXT PRIMARY KEY,                               -- stg_<ULID>
  request_id TEXT NOT NULL UNIQUE REFERENCES approval_requests(id),
  steward_id TEXT NOT NULL REFERENCES principals(id),
  matter_ref TEXT NOT NULL DEFAULT '',
  scope TEXT NOT NULL,                               -- frozen scope (share-policy specs + include_classes), JSON, copied from the payload
  scope_digest TEXT NOT NULL,
  status TEXT NOT NULL,                              -- approved | revoked
  not_before TIMESTAMP NOT NULL, expires_at TIMESTAMP NOT NULL,
  approved_at TIMESTAMP NOT NULL, revoked_at TIMESTAMP, revoked_by TEXT, revoke_reason TEXT,
  renews_id TEXT REFERENCES steward_grants(id));
ALTER TABLE ui_sessions ADD COLUMN steward_grant_id TEXT;      -- the elevation, §3
ALTER TABLE ui_sessions ADD COLUMN steward_active_at TIMESTAMP;
ALTER TABLE access_log ADD COLUMN steward_grant_id TEXT;       -- §5
ALTER TABLE access_log ADD COLUMN entry_digest TEXT; ALTER TABLE access_log ADD COLUMN prev_digest TEXT;
```

### 3. What a steward sees

**Options considered.**

| Option | Verdict |
|---|---|
| A. Time-boxed audience membership plus an ordinary `share_grants` row | Rejected. The grant is live on every request, so ordinary browsing is mixed with elevated reading and cannot be told apart in the audit; state lives in three rows (membership, grant, policy) with three expiries; editing the named policy silently widens it; and its grant would merge into the ordinary view, which on `next` is plan-wide and leaks (see Context). |
| B. Implicit union: the steward's viewer is always own view plus every active steward grant | Rejected. Same attribution gap (every read would have to log every active grant, and none would say which grant a given row came from) and the same plan-wide merge: a steward scope with `fields: {"*": ["*"]}` or `export: true` would change how objects visible only through the steward's ordinary grants render and what the whole view may do. A per-object grant merge, a prerequisite and future work, would answer the merge, not the audit gap. |
| **C. Explicit elevation, one grant per request, grant-only viewer (chosen)** | The steward's ordinary requests are the ordinary view. While elevated, the viewer is `Compile` of that one grant's specs and nothing else ("grant-only"). The audit knows which requests were elevated and under which grant, and nothing mixes. |

**Elevation.** In v1 it is session-bound: `POST /api/v1/steward/grants/{id}/elevate` stores `steward_grant_id` and `steward_active_at` on the browser session row; `DELETE` or `steward.elevation_idle` ends it. The session authenticator puts the grant ID in the principal's `Meta` (as it does for the identity and session IDs); `Resolve` reads it from there. Header or metadata elevation for API credentials waits for enrolled agents (§8). The UI shows a persistent "elevated: <matter>, expires <time>" banner.

**Resolution** (`loadInput` gains a steward branch). With a grant named, in order, any failure is a refused request with a stable check code and an audit entry, never a fallback to the ordinary view:

1. the grant exists and `steward_id` equals the principal (`grant_unknown`, `not_steward`);
2. `status = approved`, `not_before <= now < effective expiry`, not revoked (`grant_not_active`, `grant_expired`, `grant_revoked`);
3. the principal currently holds `request:steward` and is not suspended or revoked (`role_missing`), so removing the IdP group ends use at once;
4. the stored `scope_digest` still matches the canonical scope (`scope_changed`).

The same refusal carries its own code for the causes outside these checks: `steward_disabled` (the feature is off, or the instance has no steward wiring), `scope_invalid` (the digest holds but the stored scope no longer parses as a scope), `identity_unknown` (the credential the request was authenticated through is not in the registry: the ordinary path's unknown identity sees nothing, so this is a no, not unavailability), `key_policy_invalid` (the credential's bound key policy is gone or no longer validates), all denied; and `grant_unavailable` (a store could not be read), deferred. A denied refusal answers `403` (gRPC `PermissionDenied`), a deferred one `503` (`Unavailable`), with the code `STEWARD_<REASON>`. Refusals at filing are the kind's own codes, `STEWARD_REQUEST_*`.

Then `Input.Grants` holds one grant per spec of the scope, every one the steward grant's: its ID, `Audience: "steward:<id>"`, a frozen policy at version 1 named `<id>/<i>`. An elevation holds several only when `as_audience` froze several policies; "grant-only" means only that steward grant's specs. The policy key carries a `steward=<id>` marker, so an ordinary viewer and an elevated one never share a compiled-viewer cache entry, and per spec the grant ID, the spec's index and the scope digest, so `PolicyKey()`, and with it `access_log.policy_versions`, names the grant for free.

**What still applies, unchanged.**

- **The steward's key policy** (if the credential is bound to one) still intersects: keys narrow, never widen.
- **The principal's ceiling** (`principals.deny_classes`) still applies. A steward whose ceiling lists `personal` cannot receive `personal` by any grant; clearing it is an owner act (`admin:shares`).
- **Withheld classes.** `steward.withheld_classes` are added to the grant's `exclude_classes` at compile time, per grant, so they never touch an ordinary view. Unless `steward.allow_withheld` is on **and** the grant names the class in `include_classes`, they stay out.
- **Owner-only data stays owner-only.** Owner-only routes (jobs, inbox, capture payloads, events) are closed to every restricted viewer by the route table; a steward grant does not open them, and there is no setting that does. v1 has no way to grant raw job payloads.
- **Hidden means absent, the ID ledger and the canary backstop.** The elevated viewer is an ordinary restricted viewer. The backstop runs strict for elevated requests (a hit refuses with 500 instead of being stripped): a legal review must not receive a silently incomplete answer.
- **No streams, no MCP, no `events`.** `mcp` and `events` are forced off in v1. Because `GET /events` is open to every viewer on `next` (ADR-080 amendment 2026-11-20) and the `events` flag no longer closes it, an elevated session cannot open any stream: a route-level check on elevated requests refuses it at stream open (§4 says what applies when streams are admitted later).

**What must change in code**

1. `Input`/`loadInput`: the steward branch above; `Compile` unchanged except for the grant naming and the rule that every grant of an elevation is the named steward grant's.
2. `scope.go`: `stewardScopes` excluded from `Bundle(admin)`; two new bundles; `UISessionScopes` gains the two scopes; owner roles gain `steward-approver`.
3. The `steward.grant` kind on the shared approval store (§9), a `steward_grants` store for the grant half of the state machine, and `ui_sessions` columns for the elevation.
4. A `steward_grant` check in the receipt-shaped access entry (ADR-080 §9 amendment, not built on `next`) with its own reason codes; `AccessEntry.StewardGrantID`.
5. The durable write (§5), placed in the visibility middleware before the buffered response is written, not in the buffered audit writer. (A receipts change, `feat/audit-receipts`, is pending and may reshape this path; this ADR names no symbols from it.)
6. `Prune` and `housekeeping` skip entries with a `steward_grant_id`.
7. Route access table entries for the new routes, with explicit response shapes, so the route-table test (an unclassified route fails) covers them.

The plan-wide merge of ordinary grants was a separate, pre-existing defect and is fixed: after a scoped read the render layer asks the database which grants cover each released row and merges fields and masks over those grants only, each grant contributing what it renders for the object's type. An elevated viewer never merges with the ordinary view, so option C does not depend on that fix to keep the two apart. Within one steward grant it does rely on it: an `as_audience` grant holds a spec per policy, and per-object attribution is what makes it render each object as the audience's members do. A design that merged a steward grant into the ordinary view could rely on it too, but §3 option B stays rejected for the attribution gap.

### 4. Expiry and revocation

- **Fail closed.** Active means `status = approved` and `not_before <= now < effective expiry` and the steward still qualifies. Anything else, including a store error while reading the grant, refuses the request (`deferred` if the cause is unavailability, `denied` otherwise). No code path treats an unreadable grant as active.
- **Revocation is the forced end.** `expired` is computed from today's cap and clock, so raising `steward.max_duration` brings back a grant the cap had expired. An operator who wants a grant ended for good revokes it: any grant whose stored `status` is `approved` may be revoked, whatever the clock or the cap says, and a revoked grant stays refused when the cap is raised.
- **Effective within one request.** Because the viewer is resolved from the store on every request (above), a revoked or expired grant fails the next request after the write commits. The compile cache cannot serve it: the key contains the grant ID and digest, and the grant is not loaded at all once it is not active. **No invalidation hook is added.** The test is the requirement: revoke between two requests of one session, the second is refused.
- **Requests in flight** finish with the viewer they resolved. Bounded by the request; paged reads re-resolve per page request.
- **Sessions.** The session survives expiry of a grant; the elevation does not. Elevation state is read from the session row on every request, and the session's own max TTL (7 days) is independent of the grant's window.
- **Streams and long operations** (the first restricted stream, `events`, MCP with server-sent responses, and the compliance export) must re-resolve the viewer at least every `steward.recheck` (proposed 30 s) and per chunk for exports, and end on failure. None is open to a steward-elevated session in v1. `GET /events` streams to ordinary restricted viewers (ADR-080 amendment 2026-11-20), and the `events` capability flag no longer closes it, so a route-level check refuses an elevated session at stream open (owner decision). A lifecycle test asserts that an elevated session is refused at stream open. `JobService.WatchJob` stays owner-only. The recheck rule is recorded so a stream admitted later does not ship without it.
- **Any future cache** of viewer-derived data (ADR-080 §6 mentions placeholder-entity visibility) keys on `PolicyKey()`. A test fails the build if a steward-scoped cache entry survives a revocation.
- **Revoking the principal or its bundle** (account disabled, IdP group removed, membership TTL lapsed with the source unavailable, ADR-082 §10) ends use through check 3 above.

### 5. Audit

Every elevated request is attributed and durable.

- **Attribution.** The access entry carries `steward_grant_id`, the `steward_grant` check (`grant_active` on success, the codes above otherwise), the operation digest, the policy key and the usual counts and object IDs. Reason, requester and approvers are joined from the grant, not copied into every entry. Refused elevations (inactive grant, wrong principal) are recorded too.
- **Durability, options.** (A) The existing buffered writer: rejected, it loses entries on a full buffer, a failed flush and a crash, and it records after the response is out. (B) A synchronous append **before the response is released**, failing closed: chosen. (C) A write-ahead intent record before the handler plus a buffered completion: rejected as redundant, because under B nothing is released without its record. Under B, the elevated path makes a durable write from the visibility middleware (where the response is already buffered and the ledger final, before it is written to the client); if the append fails, the response is replaced by `503 AUDIT_UNAVAILABLE`, the decision is `deferred`, and nothing is disclosed. A crash after the append and before delivery over-records, which is the safe error. The cost is one synchronous write per elevated request on a path that carries a handful of requests an hour.
- **Tamper evidence.** Each steward entry stores `entry_digest = SHA-256(prev_digest || canonical entry incl. a digest of its object-ID list)`, chained in append order per instance. SQLite's single writer serialises the chain; Postgres takes a row lock on a chain-head row. The head is what the signed manifest (§6) commits to. This detects deletion or edit of entries; it does not stop an operator with database write access from rewriting the whole chain, which is why the manifest is signed and exported.
- **Retention.** Steward entries, their object-ID lists and the grant rows are exempt from `sharing.audit.retention` and follow `steward.audit_retention` (default keep). A steward-grant row is never deleted by housekeeping.
- **Who can see it.** Approvers and the owner (`approve:steward`, owner) read the per-grant report; the requester and steward read their own grant's entries (counts, routes, times, not object IDs). `admin:audit` sees the existing access log as today, which includes steward entries' routes and counts; the reason text stays with the grant and needs `approve:steward`. Whether `admin:audit` should see steward entries at all is open.
- **Owner reads** are not logged by default (ADR-080 §9). On a business instance that is the one unaudited path. Setting `sharing.audit.log_owner` for such instances is recommended and is open (see Open questions); it does not change the bypass.

### 6. Review, activity report and export

- **Per-grant activity report** (API, CLI, UI): first and last use, requests by route and day, refusals, object-ID list while retained, exports, current status and time remaining. Computed from `access_log WHERE steward_grant_id = ?`.
- **Approver preview.** When an approver opens a request they see the reason, window, steward, requester and the frozen scope, plus **counts** of what the scope matches (objects by type, source and class; no titles, no snippets), computed by the same compiler. The counts tell an approver whether "all contacts" is intended. A count is metadata, and approvers are trusted with it.
- **Compliance export.** A steward-only route, available only when the scope opens `export` and `steward.allow_export` is on. It streams the rendered view (never raw rows) as an archive, re-resolving the viewer per chunk, and produces a **signed manifest**: grant ID, scope digest, reason, requester, approvers and times, window, counts, per-file SHA-256, the audit chain head, and the signature of the instance Ed25519 key (`internal/bundle`, `dpkms key`). The manifest is a bundle-class artifact, not a PACT artifact, so the instance key (not ADR-082 §8's ES256 keys) signs it. The export itself is one elevated request: audited durably, with its object IDs.
- **Review of the review.** Grant creation, approval, rejection, revocation and role assignment go to `audit_log` and the security emitter; a pending request is visible to every approver.

### 7. Legal hold, retention and honest limits

What the instance guarantees:

- A steward reads only what the frozen scope selects, minus the steward's ceiling and the withheld classes, through the same leak-tested path as any viewer.
- Access is bounded in time, attributable, and recorded before release.
- Approvals, scope, reason and the audit chain are retained and exportable with a signature.

What it does **not** guarantee:

- **Access is not preservation.** A steward grant does not stop anyone from deleting or overwriting objects, and dedup merges (`Reinforce`) change objects. A hold that freezes objects in place needs its own decision (deletion and overwrite semantics, object history). Until then, "legal hold" here means controlled access to what exists; a litigation hold must be paired with the owner's own retention controls. This ADR claims nothing about object version history.
- **No per-document source permissions.** dpkms does not mirror SharePoint, Drive or mailbox ACLs (ADR-082 §10, out of scope). A steward scope can include material the source system would not have let that steward open. Approvers approve a dpkms scope, and the approval screen says so.
- **Masking is not available.** `Redact` is inert. An explicit steward scope that asks for it is refused (§2) and must use classes, projection and the data filter. `as_audience` carries each policy's `redact` as the members have it, inert for them and the steward alike until masking lands, then applied to the steward exactly as to the members; the payload records `masking_inert` so the approvers see masking is requested but not applied. PII detection, when it lands, is best effort (ADR-080).
- **No cross-object selectors.** `mention` and `related` are refused in policies, so "everything mentioning person X" is not expressible in v1. Scopes select by type, source, tag, profile, class, dates and metadata keys.
- **A steward who exports has the data.** The manifest proves what was released and when. It does not control copies. Watermarking and DLP are not claimed.
- **The owner bypass is unaudited by default** and is the break-glass by design.
- **Database access beats everything.** An operator with write access to the instance database can alter grants and rewrite the audit chain. The signed manifest and off-host export are the mitigation, not a prevention.

### 8. Relationship to ADR-082 and the IdP; v1 and deferred

- **IdP role mappings (ADR-082 §10).** `steward-requester` and `steward-approver` are mappable bundles, mapping authoring is owner-only (§1), and a mapping change bumps its version. Role freshness follows the IdP rules: past the membership TTL with the source unavailable, the bundle is not held, check 3 fails, and use stops. How fresh a steward's role must be at elevation time is open.
- **PACT and agents.** Agents of a steward never inherit a grant: an agent holds no grants (ADR-082 §3). v1 stewards act through browser sessions only. When enrollment enforces admission, an enrolled agent may be allowed to act under a grant if the grant says so, with the elevation carried as a consequential header covered by the operation digest, and the purpose (`…/purposes/steward/{grant}`) naming the grant. That is a later amendment, not v1.
- **Federation.** A steward grant is local to the instance; it transfers nothing to a peer (ADR-082 §12).

| | v1 | Deferred |
|---|---|---|
| Shared approval mechanism (§9), built first | yes | |
| Admin credential delivery: `credential.issue` to the owner, `identity.bind` confirmed by owner or target (§10 A) | yes | Delivery to the principal over a bound channel |
| Pending widening sharing changes, route classification, `share-approver` (§10 B) | yes | IdP mapping changes as `share.change`, with mapping authoring |
| Roles and bundles, owner-only assignment, admin exclusion | yes | |
| Grant object, frozen scope, state machine, `N`-approver | yes | |
| Session-bound elevation, grant-only viewer | yes | Header elevation for API credentials |
| Durable pre-release audit, chain, retention exemption | yes | |
| Activity report, approver preview, signed export | yes | |
| IdP mapping of the two bundles (Entra) | yes | Google, Okta, Keycloak |
| Steward agents, CLI, MCP | | After admission enforcement |
| Streams for elevated sessions (`GET /events`) | | Refused in v1 by a route-level check; admitting them needs a decision with the per-event recheck of §4 |
| Preserve-in-place hold | | Own ADR |
| Cross-object selectors, masking inside scopes | | After the policy language gains them |
| Notifying data subjects | | Policy decision per jurisdiction |

### 9. The approval mechanism (shared, owner-decided 2026-10-16)

One mechanism decides every change that needs someone other than its requester to accept it. It is built before its consumers, and each consumer is a **kind** of request on it, not a system of its own. v1 has the kinds `steward.grant` (§2) and the three kinds of §10 (`share.change`, `credential.issue`, `identity.bind`).

**What every request has.**

| Element | Rule |
|---|---|
| `id` | `apr_<ULID>`. |
| `kind`, `action` | The kind names the consumer; the action names the operation inside it (`share.grant.create`, `credential.key`, ...). |
| `requested_by` | The authenticated principal that filed it. Never changes. |
| `subject_id` | The principal the request is about (the steward, the target of a credential or binding), when there is one. |
| `reason` | Free text. Required with a minimum length where the kind says so (`steward.grant`); optional elsewhere. |
| `payload`, `payload_digest` | The operation, frozen as canonical JSON when filed and validated by the kind; SHA-256 over it. Every decision binds to the digest. |
| `preconditions` | The state the payload was frozen against (a policy's version, a membership's current expiry, a principal's status). A request whose preconditions no longer hold when the last approval arrives is not applied; it ends `stale` and the requester files again. |
| `approvals_required` | `N`, from the kind's instance setting `approvals.<kind>.required` (default `1`, `N>=1`), frozen when filed. Some kinds fix `N` (§10). |
| `expires_at` | `requested_at + approvals.pending_ttl` (proposed 72 h). |

**State machine.** Stored: `requested`, `approved`, `rejected`, `withdrawn`, `stale`. `expired` is computed from the clock (`now >= expires_at` while `requested`), never written by a sweeper, so a late job cannot keep a request alive.

| From | To | Who triggers | Condition |
|---|---|---|---|
| (new) | `requested` | Any principal the kind allows to file | The kind validates and freezes the payload and records the preconditions. |
| `requested` | `approved` | Eligible approver | The `N`-th approval by `N` distinct eligible approvers, each bound to `payload_digest`, before `expires_at`. It applies only when every recorded approval binds to the request's current `payload_digest`. The kind applies the payload in the same transaction; the request records what it produced (`result_ref`: the grant, membership, identity or key it wrote). A kind whose result is a secret (`credential.issue`) returns it in the final approver's response only, once; it is never stored or logged. |
| `requested` | `rejected` | Any eligible approver | Note recorded. Terminal. |
| `requested` | `withdrawn` | `requested_by`, or the owner | Terminal. |
| `requested` | `stale` | Shared mechanism | At the final approval, the preconditions fail or a recorded approval binds to another digest than the request's current `payload_digest`. Terminal; nothing is applied. |
| `requested` | `expired` | Clock | Terminal. |

**Separation.** Checked in the decision write, not the UI, for every kind:

- the approver is never `requested_by`;
- the approver is never a principal the request would benefit: the `subject_id` and whatever principals the kind names as beneficiaries (§10 lists them for sharing changes). The one exception is a kind whose subject is its recipient by design: `identity.bind`, where the target confirming the binding is the point (§10 A);
- the approver holds the kind's approver scope at decision time, or is the owner where the kind admits the owner;
- one principal records at most one decision per request.

**The owner** files nothing on this mechanism for its own changes: an owner change applies at once and is audited as today. The owner is an eligible approver of every kind, and the only one of some (§10).

**Audit.** Filing, each decision, withdrawal, staleness and the application write `audit_log` entries and security events naming the actor, the request and the payload digest; the change an approval applies is also recorded under its own event (`share.grant.created`, ...) with `requested_by` as the actor and the request id and approvers in the payload. Payloads never carry a secret.

**Routes.** `GET /api/v1/approvals` (requests the caller filed, may decide, or is the subject of; filter by kind and state), `GET /api/v1/approvals/{id}`, `POST /api/v1/approvals/{id}/approve`, `POST /api/v1/approvals/{id}/reject`, `DELETE /api/v1/approvals/{id}` (withdraw). A route that files a request answers `202 Accepted` with the request (`id`, `kind`, `action`, state, `expires_at`) and `Location: /api/v1/approvals/{id}`. Each route is classified in the route-access table like any other.

**Storage sketch** (instance DB, SQLite and Postgres in lockstep; non-binding):

```sql
CREATE TABLE approval_requests (
  id TEXT PRIMARY KEY,                               -- apr_<ULID>
  kind TEXT NOT NULL,                                -- steward.grant | share.change | credential.issue | identity.bind
  action TEXT NOT NULL,
  requested_by TEXT NOT NULL REFERENCES principals(id),
  subject_id TEXT REFERENCES principals(id),
  reason TEXT NOT NULL DEFAULT '',
  payload TEXT NOT NULL,                             -- frozen canonical JSON, validated by the kind
  payload_digest TEXT NOT NULL,
  preconditions TEXT NOT NULL DEFAULT '{}',
  approvals_required INTEGER NOT NULL,
  status TEXT NOT NULL,                              -- requested | approved | rejected | withdrawn | stale
  requested_at TIMESTAMP NOT NULL, expires_at TIMESTAMP NOT NULL,
  decided_at TIMESTAMP, result_ref TEXT NOT NULL DEFAULT '');
CREATE INDEX approval_requests_kind_status ON approval_requests (kind, status, expires_at);
CREATE TABLE approval_decisions (
  request_id TEXT NOT NULL REFERENCES approval_requests(id),
  approver_id TEXT NOT NULL REFERENCES principals(id),
  decision TEXT NOT NULL,                            -- approve | reject
  payload_digest TEXT NOT NULL, note TEXT NOT NULL DEFAULT '', decided_at TIMESTAMP NOT NULL,
  PRIMARY KEY (request_id, approver_id));
```

**Shape, options considered.**

| Option | Verdict |
|---|---|
| A request table per kind (`steward_grants` holding its own request states, a second table for pending admin changes), sharing nothing or only a decisions table | Rejected. Two state machines, two separation checks and two TTL computations that drift; the second consumer would copy the first. |
| **One request table with a `kind` discriminator and a frozen JSON payload; what an approval produces lives in the consumer's own table (chosen)** | One state machine, one separation check, one audit trail and one approver queue. The request lifecycle is identical across kinds; what follows approval is not (a steward grant has a window and revocation, a membership is just a row), so it stays where it already lives. Payloads are TEXT in both databases for lockstep; the kind's validator, not the schema, types them. |
| One table holding the request and the post-approval lifecycle of every kind | Rejected. A sharing change has no window and no revocation of its own (the membership or grant it writes does), so every row of that kind would carry unused columns, and steward elevation would read a generic table. |

In code the mechanism is a registry of kind handlers, each providing validation and freezing, the eligible approvers and beneficiaries, the preconditions check and the apply step; the store and the state machine are shared and know no kind.

### 10. Administering is not data access: credential delivery and pending widening changes (owner-decided 2026-10-16)

ADR-082 §10 says administering is not data access. The viewer already obeys it; the registry routes did not (Context, "Two holes"). Two rules close them. "Non-owner admin" below means any caller that is not the owner and reaches a route through `admin:principals` or `admin:shares`, by key or by session. Both rules apply only when the target is another principal: a caller's own credentials stay under `write:self` as today.

#### A. An admin never receives a working credential for another principal

Minting a key, a sign-in link or an invite, or binding an identity, for another principal delivers the secret only to the owner or to that principal through a path the principal already controls, never in the admin's response. Admins keep everything that only takes away: suspend, revoke, remove an identity, revoke a key. They keep reading credential metadata (ids, names, scopes, times), which already carries no secret (a future leak-suite fixture asserts it).

Mechanically, **the admin authorizes and the recipient mints.** The admin's call files a request on the shared mechanism (§9) whose payload describes the credential (target, key name, scopes, expiry; invite display name and audience; identity method, issuer, subject) and holds no secret. The secret is generated only inside the recipient's approve response, shown once, and never stored in plaintext. Nothing is minted before then, so there is no undelivered secret at rest to protect.

| Route (HTTP; no gRPC equivalent exists) | Owner caller | Non-owner admin, target another principal |
|---|---|---|
| `POST /principals/{id}/keys` | Unchanged: `201` with `key`. | `202`, request kind `credential.issue`, action `credential.key`. Approver: the owner only, `N = 1`. The owner's approve response carries the key; the owner hands it over out of band. Where PACT enforcement is on (ADR-082 §11), person targets are refused before any request is filed. |
| `POST /principals/{id}/links` | Unchanged: `201` with the URL. | `202`, `credential.issue` / `credential.link`, owner only. The owner's approve response carries the URL, valid `MagicLinkTTL` from that moment. |
| `POST /share/invites` | Unchanged: `201` with the URL. | `202`, `credential.issue` / `credential.invite`, owner only. The payload includes the audience, so the owner's approval is also the approval of the membership the invite carries (B). |
| `POST /principals/{id}/identities` | Unchanged: `201`, the binding is live. | `202`, kind `identity.bind`. Approvers: the owner, or the **target principal itself** from a session or key it already holds (`approve` on the request as its subject; it is the one approver for whom being the subject is the point). `N = 1`. On approval the identity row is written. Until then the login authenticates nothing. |
| `DELETE /principals/{id}/identities/{identity}`, `DELETE /principals/{id}/keys/{key}`, `POST /principals/{id}/suspend`, `POST /principals/{id}/revoke` | Unchanged. | Unchanged: they only take away and apply at once. |
| `GET /principals/{id}`, `GET .../identities`, `GET .../keys` | Unchanged. | Unchanged: metadata only. |
| `POST /principals` | Unchanged. | Unchanged: a new principal has no credential and, roles never deciding a viewer, no view. |
| `/self/keys`, `/self/identities`, `/self/sessions` | Unchanged. | Unchanged: the caller's own. A request on `/principals/{id}/...` with `{id}` the caller is treated as its `/self` form. |
| `POST /ui/login-codes` | Unchanged. | Unchanged: a login code signs a browser in as the caller itself. |

For `credential.issue` the owner is the only eligible approver because nobody else can be the recipient: the target of a key or link has, by assumption, no credential through which to receive it (a principal that has one mints its own under `write:self`), and an invite has no principal yet. A later delivery channel bound to the principal (a verified address on the principal, set by the IdP or the owner, never by a non-owner admin) could add the principal as a recipient; it does not exist today (deferred, Open questions).

**How a first credential reaches a new principal.**

- **Business instance (IdP authority, ADR-082 §10).** A person's first credential is their own IdP sign-in: dpkms creates the `person` principal and its `oidc` identity on the first admitted request, so no admin ever mints or binds one. Invites and sign-in links are not the onboarding path there. A device's or peer's first key comes from the owner (directly, or by approving an admin's `credential.issue` request); for agents, enrollment (ADR-082 §3) becomes the path once it exists. The browser sign-in ceremony for OIDC is reserved and not implemented (`config.ReservedAuthSignIns`), so business onboarding waits on it.
- **Personal instance (local owner).** The owner creates principals and their first credentials, as today. A non-owner admin the owner appointed can prepare everything (principal, roles, audiences, a credential request) and the owner completes it with one approval.

**Consequence for ADR-082 §3.** Enrollment "by `admin:principals`" for another principal falls under this rule: an admin must not end up holding an enrolled agent of another principal. An admin-requested enrollment for another principal is confirmed by that principal's own human path or by the owner. The form is left to the enrollment design (Open questions).

#### B. Widening sharing changes by a non-owner admin wait for approval

A sharing change by a non-owner admin that **widens** anyone's access files a `share.change` request (§9) and changes nothing until approved; a change that narrows or changes no view applies at once, as today. Owner changes are never pending.

**Definition, decidable by the server.** For a change *c* applied to the current registry state *s*, *c* is

- **neutral** if no viewer's compiled view differs between *s* and *c(s)* because no grant in force reaches what *c* touches;
- **narrowing** if, for every viewer, every row and every field visible after *c* was visible before it, and every capability held after was held before;
- **widening** otherwise, and **whenever the server cannot show the first two** (an unparseable spec, an RSQL relation it cannot compare, a store error while classifying).

The server never evaluates data to classify: it compares the change against the current registry rows with the structural rules below, in the same transaction as the write, so a concurrent change cannot slip between the classification and the write. The transaction alone does not give that on Postgres, where READ COMMITTED lets a concurrent commit land between a read and a write: the classification and the write (and, for an approval, the precondition check and the apply) run under the registry lock, a Postgres advisory transaction lock (SQLite's write lock, taken when the transaction begins, is the same exclusion), so no other change made through the sharing routes or an approval commits in between.

**Policy spec comparison** (used by a policy update and a key-policy rebind). A new spec is narrower than or equal to the old one when all of these hold, else it is widening:

- `data` (required in every policy, `Policy.DataNode`): canonically equal to the old filter, or a conjunction (`;`) one of whose operands is canonically equal to the old filter;
- `exclude_classes`: a superset of the old list;
- `fields`: for every type either spec names (the `*` entry included, which stands for every type neither names), the fields the new spec renders for it (its own entry, else its `*` entry; the `*` field is every field) are a subset of those the old spec renders for it. Nothing more is asked of which types a spec names. An object renders the union, over the grants that cover it, of what each grant renders for the object's type (under the per-object grant merge, a prerequisite and built: the fallback is resolved per grant before the union, and only covering grants contribute), so a spec that renders no more of any type renders no more of any object, alone or merged with other grants;
- `capabilities`: `search` a subset; each of `entities`, `export`, `mcp`, `events` on only if it was on;
- `redact` (inert today, compared so that it stays safe when masking lands): `mask` a superset, `reveal` a subset, `style` unchanged.

**Route classification.**

| Route | Class | Beneficiaries (may not approve) |
|---|---|---|
| `POST /share/audiences` | Neutral: an empty audience has no grant. | |
| `PUT /share/audiences/{a}/members/{p}`, no current membership | **Widening.** | `p` |
| same, membership exists | Widening if the new expiry is later than the current one or removes it; narrowing if it is earlier; neutral if equal. | `p` |
| `DELETE /share/audiences/{a}/members/{p}` | Narrowing. | |
| `POST /share/policies`, `POST /share/templates/{name}` | Neutral: a policy reaches no viewer until granted. | |
| `PUT /share/policies/{p}` | Neutral if no grant of `p` is in force; else narrowing or widening by the spec comparison. A pending grant of `p` records `p`'s version as a precondition, so an edit makes it `stale` instead of widening it silently. | Members of every audience holding a grant of `p` in force |
| `POST /share/grants` | **Widening**, always: the audience's members gain the policy's rows, and its fields on every object they already see through another grant that it covers too. | Members of the audience, local and external |
| `DELETE /share/grants/{g}` | Narrowing. | |
| `PUT /principals/{id}/ceiling` | Narrowing if the new `deny_classes` is a superset of the current one; neutral if equal; **widening** otherwise. | `id` |
| `PUT /share/keys/{key}/policy`, unbound to bound | Narrowing. | |
| same, bound to unbound | **Widening.** | The key's principal |
| same, bound to another policy | Narrowing if the new policy's spec is narrower than or equal to the old one; **widening** otherwise. | The key's principal |
| `POST /share/invites` | `credential.issue` (A), owner only; covers the membership it carries. | |
| `PUT /self/keys/{key}/policy` | Not an admin change: a key's view never exceeds its own principal's, which grants and the ceiling decide. Applies at once. | |
| IdP role and group mappings (ADR-082 §10), when their authoring route lands | A new mapping version is widening if any group it maps yields an audience membership or a bundle the previous version did not; else narrowing or neutral. | Principals the new yield reaches |
| Every read (`GET`) | Not a change. | |

A widening change answers `202` with the request (§9). The payload is the route's request body plus the target ids; the preconditions are what the classification read (the membership's expiry, the policy version, the ceiling, the key's binding), and a change in any of them makes the request `stale`.

**Approvers.** The owner, and holders of a new scope `approve:shares` in a bundle `share-approver` that, like the steward bundles, `admin` does not contain (§1's exclusion list gains it, `UISessionScopes` gains it, the owner holds it). Only the owner assigns the bundle or maps an IdP role to it. Never the requester and never a beneficiary in the table above, computed at decision time. `N` is `approvals.share.required` (default `1`). TTL, audit and withdrawal are §9's.

**Options considered.**

| Option | Verdict |
|---|---|
| Refuse only self-targeted changes (an admin may not add itself to an audience, bind its own key, set its own ceiling) | Rejected. Bypassed at one remove: add a sibling principal the admin controls, or mint an invite into the audience and redeem it, and read through that principal. |
| Cap every grant at the granting admin's own view | Rejected. An admin's view is empty by design (ADR-082 §10), so business admins could not administer sharing at all. |
| Pending approval for any change by a non-owner admin, narrowing included | Rejected. Revocation and narrowing are how an admin reacts to an incident; delaying them protects nothing. |
| Approval by any other `admin:shares` holder | Rejected. Two admins approve each other's self-grants; an approver bundle outside `admin` keeps the choice of approvers with the owner, as for stewards. |
| **Credentials to the recipient only (A), widening changes pending on the shared mechanism (B) (chosen)** | Closes both holes without a new visibility rule; reuses the steward approval design. |

---

## Rationale

- **A grant, not a role,** because a role cannot be time-boxed, cannot name a justification and cannot be attributed per read. The owner decision demands all three.
- **Explicit elevation over a standing union** because auditability is the product. "Every steward read is recorded with grant ID and reason" is checkable only when the system knows which requests were steward requests. A per-object merge (future work) would keep the merge from leaking, but the union still cannot say which grant a given row came from.
- **Frozen scope over a reference** because an approval is an approval of specific text. A named policy that can be edited later turns approval into a pointer.
- **Computed state over a sweeper** because the failure mode of a late job is an expired grant that still works.
- **Synchronous record before release** because the whole purpose is that a steward cannot read without leaving evidence. The cost is small and the volume is low.
- **Owner-only assignment falls out of rules we already have.** The ceiling rule (no route yields more than the caller holds) plus an `admin` bundle that does not contain the steward scopes means no further rule is needed to keep `admin` from handing out stewardship.
- **One approval mechanism, not two,** because the hard parts (separation, frozen payloads, TTL, staleness, audit) are the same for a steward grant and a widening sharing change, and a second copy would drift from the first. Building it first means neither consumer ships with its own.
- **The recipient mints** because a secret that is never generated for the admin cannot leak to the admin, and nothing undelivered sits at rest.
- **Widening, not self-targeting, is the test** because who benefits from a change can always be moved one principal away; what a change does to views cannot.
- **Undecidable is widening** because the cost of a false "widening" is one approval, and the cost of a false "narrowing" is the hole this ADR closes.

### Alternatives considered

| Alternative | Why not |
|---|---|
| A "compliance admin" or "data steward" role | The decision this ADR implements rejects it. |
| Owner approves everything, no separate approver role | The owner is a single break-glass account; a business needs approvers other than it, and the IdP provides them. |
| Grant through a time-boxed audience membership | §3 option A. |
| Always-on union with ordinary grants | §3 option B. |
| Buffered audit with a "best effort" label | Records after release and can drop (`audit.go`); unacceptable for the one read class that exists to be audited. |
| Steward reads as the owner (`--as`-style bypass with logging) | Makes the owner bypass broader than "the local owner"; the steward would see owner-only data. |
| Separate approval systems for stewards and for admin changes | §9: one mechanism, kinds on it (owner-decided). |
| Refuse self-targeted sharing changes only; cap grants at the admin's own view | §10 B options. |

---

## Consequences

### Positive

- A business instance can open data for compliance without a role that sees everything, with approval, a time limit and a durable record.
- The mechanism reuses policies, `Compile`, the scoped driver, the render layer and the backstop; the leak suite covers it as another viewer.
- Revocation and expiry need no cache plumbing; they fall out of per-request resolution.
- "Administering is not data access" holds at the registry as well as in the viewer: a non-owner admin can neither grant itself a view nor sign in as someone else, directly or through a sibling principal or an invite.
- One approval queue, one audit trail and one separation check for every change that needs a second person.

### Negative

- Elevated reading has friction: request, approval, elevate, idle timeout. That is the point; it is also an adoption cost for small teams (set `N = 1`).
- Every widening sharing change by a non-owner admin waits for an approver. A business that wants admins to share freely must still appoint `share-approver` holders; there is no setting that turns approval off.
- Device and peer keys, sign-in links and invites requested by a non-owner admin need the owner. On a business instance the owner is break-glass and may be rarely present, so persons must onboard through IdP sign-in, which is not built yet (OIDC sign-in is reserved).
- Some routes now answer `202` with a request instead of `201` with a result; the CLI and web UI must show pending requests.
- A synchronous audit write sits on the elevated request path, and a failing audit store takes steward reading down (fail closed).
- v1 stewards are browser-only. Scripted eDiscovery waits on agent enrollment.
- The audit chain adds writer coordination on Postgres.

### Neutral or considerations

- Ordinary shared viewers (ADR-080) are unchanged.
- `private` instances have one principal and no stewards.
- The owner remains the only bypass and the only unaudited reader unless owner logging is turned on.

### Risks

| Risk | Mitigation |
|---|---|
| Approvers rubber-stamp | Mandatory reason, frozen scope with counts, `N = 2`, per-grant activity report, anomaly review by the owner |
| A scope is broader than intended | Frozen and digest-bound; counts shown; withheld classes; ceiling; short windows; revoke at any time |
| IdP admin escalates to steward | `admin` excludes the scopes; assignment and mapping authoring are owner-only; invariant test |
| Audit store outage blocks legitimate review | Intended fail-closed; owner break-glass; the store is the instance DB, not a new dependency |
| Steward exfiltrates | Not preventable by dpkms (§7); signed manifest, export audit, short windows, legal controls |
| Clock skew moves a boundary | One server clock decides; tests with an injected clock at the boundaries |
| A widening change is classified as narrowing | Undecidable is widening; classification runs in the write's transaction; a table test classifies every registry write route (an unclassified one fails), and each structural rule is mutation-tested |
| Approvers collude with the requester | Approver is never requester or beneficiary; `share-approver` assignment is owner-only; `N = 2`; every application is in `audit_log` with requester and approvers |
| A pending change is applied against a registry that moved | Preconditions frozen with the payload; a mismatch ends the request `stale`, nothing applied |
| An admin binds a login it controls to another principal | `identity.bind` is applied only by the owner or by the target from a credential it already holds |

---

## Implementation plan

Follow-up work maps onto the sections as below, in this order: the shared mechanism first, then its two consumers on top. Names are descriptive, not task IDs.

1. **Invariant tests first.** `admin` ∩ {steward scopes, `approve:shares`} is empty across every bundle and mapping fixture; none of them is in a `reader`/`writer` bundle; owner holds `steward-approver` and `share-approver` but cannot request a steward grant; an unclassified steward or approval route fails the route-table test.
2. **Shared approval mechanism (§9).** Migrations for `approval_requests` and `approval_decisions` (SQLite and Postgres in lockstep), the store, the kind-handler registry, the state machine with computed `expired`, separation (requester, beneficiaries, approver scope, one decision each), frozen `N`, payload digests and preconditions with `stale`, the `/approvals` routes, `audit_log` and security events, the approver queue in the web UI and `ctxt approval` commands. Tested with a test-only kind before any real kind exists.
3. **Admin credential delivery and pending widening changes (§10).** The kinds `credential.issue`, `identity.bind` and `share.change` on the mechanism; the owner path unchanged; `202` answers on the routes of §10 A and on the widening rows of §10 B; the structural policy-spec comparison and the per-route classifier, with a table test over every registry write route; `approve:shares` and `share-approver`; the CLI (`ctxt principal`, `ctxt share`) and the Sharing page showing pending requests instead of secrets. The leak suite gains the two attack sequences as fixtures: self-grant (audience, self-membership, match-all policy, grant), the same through a sibling principal and through an invite, and impersonation (key, link, identity binding for another principal); each must leave the admin's view empty until an eligible approver accepts, and each check is mutation-tested.
4. **Grant storage, state machine and viewer compilation.** The `steward.grant` kind on the mechanism, the `steward_grants` table, the grant half of the state machine, computed `active`/`expired`, `loadInput` steward branch, `Compile` input, withheld classes, forced-off capabilities.
5. **Request, approval and elevation flow.** Steward API routes, `ctxt steward` commands (owner and enrolled agents later), web UI request/elevate screens with the banner and idle timeout, notifications through the security emitter.
6. **Elevated-read audit.** `steward_grant` check and reason codes, `AccessEntry.StewardGrantID`, durable pre-release write in the visibility middleware with `503 AUDIT_UNAVAILABLE`, chain digests, prune and housekeeping exemption.
7. **Compliance search and export.** Report endpoint, approver preview counts, export route with per-chunk re-resolution and signed manifest.
8. **IdP role mapping.** The mappable bundles for Entra, owner-only authoring, mapping version bumps, role freshness at elevation, and mapping changes classified as `share.change` (§10 B).
9. **Leak and lifecycle suite.** The steward fixture principals added to the existing leak suite: ordinary view unchanged by an active grant; elevated view equals the grant's scope exactly (differential run with and without hidden data); expired, revoked, not-yet-active, wrong-steward, role-removed and tampered-scope grants each refuse on the next request; audit failure refuses and discloses nothing; a revoked grant cannot be served from the compile cache; every new check is mutation-tested.
10. **Operator guide.** Enabling the feature, choosing `N` and the caps, assigning bundles, mapping IdP roles, what approvers see, how pending admin changes and credential requests reach the owner, reading the report and verifying a manifest, and the honest-limits page (§7).

---

## Open questions (not decided here)

1. **Numbers.** `max_duration`, `approvals.pending_ttl` (and whether it should differ per kind), `elevation_idle`, `steward.recheck`, `min_reason_chars` and the default `withheld_classes` are proposed, not settled. Also whether `max_duration` has an absolute ceiling the setting cannot exceed.
2. **Review without approval.** Whether a compliance auditor role may read reports and manifests without being able to approve; today review rides on `approve:steward` and the owner.
3. **`admin:audit` and steward entries.** Whether they stay in the generic access log view for `admin:audit` holders, or are visible to steward reviewers only.
4. **Owner logging on business instances.** Whether `sharing.audit.log_owner` becomes the default when an IdP is configured, and what it records.
5. **Role freshness at elevation.** How recently the IdP must have confirmed a steward's bundle, and how that composes with ADR-082's per-source freshness.
6. **Preserve-in-place hold.** Whether and how a grant (or a separate hold object) freezes deletion and overwrite for its scope, and what object history the instance must keep.
7. **Cross-object selectors.** Whether policies gain a bounded form of `mention` for steward scopes (custodian-style "everything about person X"), given ADR-080's reasons for refusing it.
8. **Dual-role principals.** Whether one principal may hold both bundles; the per-grant separation rule already prevents self-approval.
9. **Steward agents.** The form of header elevation, purpose naming and per-grant agent permission once admission is enforced.
10. **Notification.** Who is told when a grant is requested, approved or first used, over which channel, and whether data subjects are ever notified.
11. **Chain trust root.** Whether the audit chain head is also anchored off the host on a schedule, beyond the signed manifest.
12. **Fix ordering of the grant merge.** Done: per-object grant attribution in render replaced the plan-wide merge before any steward grant is elevated.
13. **Delivery to the principal.** Whether a channel bound to the target principal (an address the IdP verified or the owner set, never one a non-owner admin set) becomes a second recipient of `credential.issue`, so a business instance does not need its owner for device keys and links.
14. **Enrollment on the shared mechanism.** Whether ADR-082 §3's enrollment approval becomes a kind on §9's mechanism, and how an enrollment an admin requests for another principal is confirmed by that principal (§10 A).
15. **Admin-bound key policies.** A principal can unbind, under `write:self` (`PUT /self/keys/{key}/policy`), a policy an admin bound to narrow its key. Whether an admin-bound policy should be removable only by an admin or the owner.
16. **Spec comparison strength.** Whether the structural comparison of §10 B grows (for example implication between simple RSQL comparisons) so that fewer genuine narrowings go pending.

---

## References

- ADR-079 (§1 principals, §5 scopes, bundles and the ceiling rule), ADR-080 (§2 policies, §4 hidden means absent, §8 backstop, §9 audit), ADR-082 (§3 agents, §10 enterprise IdP and the no-data-admin rule, §11 person-key hold), ADR-064, ADR-075, ADR-077
- Plan: [`docs/plans/2026-10-14-auth-and-sharing.md`](../plans/2026-10-14-auth-and-sharing.md)
- `internal/sharing/{resolver.go,compile.go,render.go,scoped.go,audit.go,policy.go}`, `internal/auth/{scope.go,ticket.go}`, `internal/server/http/{middleware_auth.go,middleware_visibility.go,capabilities.go,routes.go,handlers_principals.go,handlers_share.go,handlers_share_policies.go,ui_ceremony.go}`, `internal/server/grpc/{auth.go,capabilities.go}`, `internal/storage/{sharing.go,sqlite/sharing.go}`, `internal/bundle`, `internal/config/sharing.go`, `cmd/dpkms/cmd/housekeeping.go`

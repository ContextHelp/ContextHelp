# ADR-084 – dpkms Operator Verbs Stay Plain `/api/v1` Routes; No kit Command Projection

> **Status:** Not adopted
> **Date:** 2026-11-21
> **Author:** jadb
> **Applies to:** dPKMS HTTP and MCP surfaces; the ctxt client path
> **Supersedes:** None
> **References:** ADR-023 (authentication and authorization), ADR-077 (ctxt is a pure API client), ADR-079 (principals and the kit/ctxt split), ADR-080 (sharing and per-viewer visibility)
> **Plan:** [`docs/plans/2026-09-27-ctxt-remote-client.md`](../plans/2026-09-27-ctxt-remote-client.md)

---

## Question

Should dpkms serve its operator verbs through hop.top/kit's command projection (`cmdsurface.MountProjection`, plus `mcpsdk`) beside the `/api/v1` resource routes, so one cobra definition yields REST and MCP?

## Decision

**No.** The verbs still unserved on `next` are lint and doctor, `upgrade plan` and `upgrade run`, and the embeddings lifecycle (list, provider, register, set-default, deprecate, purge, migrate). Each becomes a plain `/api/v1` route plus a ctxt CLI call, as the ADR-077 plan already specifies (lint: lines 338-353; embeddings: 472-497; upgrade plan and run: 516-534). Admin verbs are not exposed over MCP.

## Why

- **One route table.** `apiRoutes` is "the only place /api/v1 routes are declared" (`internal/server/http/routes.go:12`). `RequireScope` (`internal/server/http/scope.go:25`) wraps every entry and a test walks the router (`internal/server/http/scope_test.go:73`). `RequireCapability` and `routeOwnerOnly` close routes to restricted viewers (`internal/server/http/capabilities.go:32,46,281-290`, ADR-080). A mounted projection sits outside that table and would need all of it re-wired, then again for the PACT and steward-grant gates of ADR-081/082 once they land.
- **Two costs that remain.** kit's refusals use a second error envelope (F12) and an unknown query parameter is silently dropped (F4), where dpkms handlers answer `INVALID_PARAM`.
- **What it would buy is not wanted.** The main gain is MCP and discovery for the verbs. They are owner and admin operations; agents should not get them.
- **Shape is cheap already.** About twelve endpoints, each a handler, a table row and a client method, as for the verbs already shipped.

## Already served as resource routes

| Verbs | Route (`routes.go`) | Handler |
|---|---|---|
| stats | `GET /stats` (119) | `Stats` |
| pages, refresh | `/pages`, `/pages/{slug}`, `/pages/{slug}/refresh` (113-115) | `ListPages`, `GetPage`, `RefreshPage` |
| topic index | `/topic-index`, `/topic-index/{topic}` (116-118) | `GetTopicIndex`, `GetTopicEntries`, `RebuildTopicIndex` |
| job cancel | `POST /jobs/{id}/cancel` (83) | `CancelJob` |
| registry sync, pending | `/entities/registry-sync` (102), `/entities/registry-pending` (107) | `SyncRegistryEntities`, `ListPendingEntities` |
| audit log | `GET /audit-log` (207) | `ListAuditLog` |
| pipelines, steps | `/pipelines` (122-123), `/steps/install` (133) | `CreatePipeline`, `ListPipelines`, `InstallStep` |
| reminders | `GET /system/reminders` (159) | `ListReminders` |

## Evidence

Probed against `hop.top/kit@v0.5.0-beta.16` with a throwaway module outside the repo: a factory-built cobra tree (`embeddings list`, `embeddings set-default <model_id>`, annotated `kit/permissions` and `kit/side-effect`), mounted with `MountProjection` behind `http.StripPrefix("/api")` and `WithProjectionAuth`, and with `mcpsdk.Mount`. Results use kit's own numbering of findings.

| # | Finding in alpha.15 | beta.16, observed |
|---|---|---|
| F1 | Projection not embeddable | Fixed: exported `MountProjection(bridge, api.Router)`; the router is an `http.Handler` and served inside a host mux |
| F2 | Constant `/v1/commands` prefix | Still true: discovery reports `"prefix":"/v1/commands"`; mounting needs `StripPrefix` |
| F4 | Unknown GET query parameter dropped | Still true: `?bogus=1` answered 200 |
| F8 | Positional arguments unusable over MCP | Fixed when the command declares `kit/args`: the tool takes `args[]` and `set-default m2` ran; undeclared, the tool says it cannot be called |
| F9 | MCP hints and global flags | Partly fixed: global flags gone from the input schema; `readOnlyHint` still false on a `kit/side-effect: read` leaf |
| F10 | No identity in MCP calls | Fixed: `mcpsdk.WithCallMeta` supplies the caller and scopes |
| F11 | `kit/permissions` unenforced | Fixed: a reader lacking `admin:embeddings` got 403 `insufficient_scope` over REST and over MCP; a holder ran the call |
| F12 | Second error envelope | Still true: `{"status","code","message"}` against dpkms's `{"error":{...}}` |

Not re-verified against beta.16, so not relied on here: F3, F5-F7, F13-F18 and the claim that kit ships no remote runner.

On `next`, ctxt consumes another service's kit projection as a client (`internal/bosfeed/bosfeed.go:38,57`, `internal/bosingest/rest.go:25,62`); it serves none.

kit beta.16 requires Go 1.27.4 or later; ctxt's `go.mod` declares 1.26.1, so any kit bump to beta.16 carries a toolchain bump.

## Revisit if

- agents need admin verbs over MCP, or
- kit closes F4 (unknown parameters refused) and F12 (host-supplied error envelope), and the route-table gating above can cover a mounted projection.

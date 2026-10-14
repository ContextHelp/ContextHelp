# Aliasing Plugin

The aliasing plugin maps human-readable names to knowledge object IDs. The
alias REST routes work without it: they read and write the core `aliases`
table directly (see [REST routes](#rest-routes)). The plugin itself is
**loaded by neither the `ctxt` nor the `dpkms` binary**, so what only the
plugin adds (its config and `ResolveID` for transparent lookup) is unused
today. This page describes what exists and what is missing.

---

## Status

| Piece | State |
|-------|-------|
| Plugin module `plugins/aliasing/` | Exists (separate Go module, listed in `go.work`) |
| `aliases` table | Exists (SQLite migration `010_aliases.sql`; Postgres baseline table, foreign key from migration 23). Deleting an object deletes its aliases on both backends |
| REST routes `/api/v1/aliases` | Working: served by `Service` over the core alias store, plugin or not |
| Plugin registration at startup | Not implemented: nothing creates `Service.PluginRegistry` outside tests |
| `ctxt alias` command | Not available: `plugins/aliasing/cli.go` defines a cobra subtree, but no binary mounts it |
| Alias resolution in other commands (`ctxt show`, `ctxt edit`, ...) | Not implemented: no core code calls `ResolveID` before an object lookup |
| Config | None: the `plugins:` config key is parsed but nothing reads it |

Entity aliases are a different feature: entities carry an `aliases` list, and
`ctxt resolve @<slug>` and `ctxt entity` match a slug or an entity alias. See
`ctxt resolve --help`.

---

## REST routes

Defined in `internal/server/http/routes.go` and
`internal/server/http/handlers_aliases.go`; the logic is in
`internal/service/aliases.go`. Full reference: the Aliases section of
[api-rest.md](../api/api-rest.md#aliases).

| Method | Path | Scope | Body / query |
|--------|------|-------|--------------|
| `POST` | `/api/v1/aliases` | `write:objects` | `{"alias", "object_id", "scope", "profile"}`; `scope` defaults to `global` |
| `GET` | `/api/v1/aliases` | `read:objects` | `?object_id=` filter |
| `GET` | `/api/v1/aliases/{alias}` | `read:objects` | `?profile=` |
| `DELETE` | `/api/v1/aliases/{alias}` | `delete:aliases` | `?scope=` (default `global`), `?profile=` |

The routes do not go through the plugin registry. A create stores a row and
answers 201 with it; an unknown object is a 404, a taken key a 409. Resolving
or deleting an alias that does not exist is a 404. A storage backend without
an alias store answers 501 `NOT_IMPLEMENTED` on every route.

---

## Plugin behaviour (when registered)

What the code in `plugins/aliasing/` does if a host registers and initialises it:

- **Scopes:** `global` or `profile`. The table key is `(alias, scope, profile)`.
- **Resolution order** (`AliasStore.Resolve`): a profile-scoped alias for the
  given profile, then a global alias. If neither exists, `ResolveID` returns the
  input unchanged and no error.
- **Init config** (the `map[string]interface{}` passed to `Init`, parsed by
  `ConfigFromMap` in `plugins/aliasing/config.go`):

  | Key | Default | Meaning |
  |-----|---------|---------|
  | `enabled` | `true` | `false` makes `ResolveID` a pass-through |
  | `default_scope` | `global` | Scope used by `SetAliasOp` when none is given; must be `global` or `profile` |

  No config file key delivers this map today.

### Key files

| File | Purpose |
|------|---------|
| `plugins/aliasing/plugin.go` | `Plugin`: `Init`, `ResolveID`, `SetAliasOp`, `ListAliases`, `RemoveAlias` (the REST routes call none of them) |
| `plugins/aliasing/store.go` | `SetAlias`, `ResolveAlias` over `pluginapi.AliasStore` |
| `plugins/aliasing/config.go` | `AliasConfig`, `DefaultConfig`, `ConfigFromMap` |
| `plugins/aliasing/cli.go` | Unmounted cobra subtree (`alias set/list/remove`) that calls the REST routes; a non-2xx answer fails the command with the server's error code and message |
| `internal/service/aliases.go` | `CreateAlias`, `ListAliases`, `ResolveAlias`, `DeleteAlias`: what the REST routes run |
| `internal/storage/sqlite/aliases.go` | SQLite `AliasStore` |
| `internal/storage/sqlite/migrations/010_aliases.sql` | Table schema |

### Running tests

```bash
go test ./plugins/aliasing/...
```

`go.work` includes the module, so this runs from the repository root.

---

## See also

- [Plugin authoring guide](../plugin-authoring-guide.md) — the plugin API and its current wiring state
- `pkg/pluginapi/pluginapi.go` — `Alias`, `AliasStore`, `AliasResolver`

# TODO Stubs Implementation Design

**Date:** 2026-02-18
**Scope:** 31 unimplemented TODO stubs across 12 command files in `cmd/ctxt/cmd/`

## Approach

All commands go through `internal/service.Service`, which already provides methods for ~70% of operations. A shared `helpers.go` in the command package provides storage initialization, output formatting, and filter building.

## Shared Infrastructure

**New file: `cmd/ctxt/cmd/helpers.go`**

| Helper | Purpose |
|--------|---------|
| `newService() (*service.Service, func(), error)` | Creates storage driver, job queue, pipeline registry, search engine; returns service + cleanup |
| `outputJSON(w io.Writer, v any) error` | Indented JSON output when `--json` flag set |
| `printTable(w io.Writer, headers []string, rows [][]string)` | Column-aligned text table via `text/tabwriter` |
| `buildObjectFilter(cmd *cobra.Command) storage.ObjectFilter` | Reads common viper flags → populated filter |

A `--json` persistent flag is added to the root command.

## Command Implementations

### Tier 1 — Config (2 stubs)

- **`runConfigShow`**: Read `cfg` global + `config.GetConfigPath()`, format key-value pairs. JSON: full config struct.
- **`runConfigValidate`**: Call `config.Load()`, report valid or list errors.

### Tier 2 — Core CRUD (4 stubs)

- **`runOpen`**: `svc.GetObject(ctx, id)` → format all fields. `--raw`/`--json` → JSON.
- **`runList`**: `buildObjectFilter(cmd)` → `svc.ListObjects` or `svc.SearchObjects` (if `--q`). Table: ID, title, type, created.
- **`runDelete`**: `buildObjectFilter(cmd)` → list matching → confirm → `svc.DeleteObject` each → report count.
- **`runEdit`**: `svc.GetObject(ctx, id)` → apply flag updates → `svc.UpdateObject` → confirm.

### Tier 3 — Entities (4 stubs)

- **`runEntitiesList`**: `svc.ListEntities(ctx, filter)` → table: slug, title, namespace.
- **`runEntitiesShow`**: `svc.GetEntity(ctx, slug)` → format details.
- **`runEntitiesSearch`**: `svc.SearchEntities(ctx, query, limit)` → table.
- **`runEntitiesBacklinks`**: `svc.EntityBacklinks(ctx, slug)` → table of linked objects.

### Tier 4 — Search (1 stub)

- **`runFind`**: `svc.SearchObjects(ctx, query, limit, 0)` → table: ID, title, type.

### Tier 5 — Jobs (5 stubs)

- **`runJobsList`**: `svc.ListJobs(ctx, filter)` → table: ID, type, status, pipeline, created.
- **`runJobsStatus`**: `svc.GetJob(ctx, id)` → format all fields.
- **`runJobsLogs`**: `svc.GetJob(ctx, id)` → display Error field.
- **`runJobsRetry`**: `svc.RetryJob(ctx, id)` → confirm.
- **`runJobsCancel`**: `svc.CancelJob(ctx, id)` → confirm.

### Tier 6 — Profiles (5 stubs)

Config-based CRUD (no separate storage backend):

- **`runProfileList`**: Read `cfg.Profiles` → table: name, default indicator, description.
- **`runProfileShow`**: Read profile by name → format tags/entities with boost weights.
- **`runProfileCreate`**: Parse config file or flags → add to `cfg.Profiles` → `config.Save()`.
- **`runProfileDelete`**: Remove from `cfg.Profiles` → `config.Save()`.
- **`runProfileSetDefault`**: Update `cfg.Profile.Default` → `config.Save()`.

### Tier 7 — Registry (5 stubs)

- **`runRegistryList`**: `svc.ListRegistries(ctx)` → table: name, URL, status.
- **`runRegistryAdd`**: `svc.FetchRegistry(ctx, url)` → confirm cached.
- **`runRegistryRemove`**: Remove from storage and config.
- **`runRegistryInfo`**: Get cached manifest → format details.
- **`runRegistrySync`**: `svc.UpdateRegistry(ctx, url)` → confirm.

### Tier 8 — Housekeeping (4 stubs)

- **`runVacuum`**: Direct `VACUUM` SQL on SQLite driver.
- **`runReindex`**: Rebuild FTS indexes.
- **`runCompact`**: Vacuum + `PRAGMA optimize`.
- **`runPrune`**: Filter by date → delete matching objects → report count.

### Tier 9 — Make / Composition (1 stub)

- **`runMake`**: `buildObjectFilter(cmd)` → `svc.ListObjects` → `svc.Compose(ctx, objects, type)` → stdout or `--output-file`. Compose uses markdown concatenation as fallback (no LLM yet).

## New Service Methods

| Method | Package | Purpose |
|--------|---------|---------|
| `CancelJob(ctx, id) error` | `internal/service` | Set job status to cancelled |
| `SearchEntities(ctx, query, limit) ([]*Entity, error)` | `internal/service` | Name/title entity search |
| `Compose(ctx, objects, type) (string, error)` | `internal/service` | Markdown concatenation fallback for composition |

## New Infrastructure

| Item | Package | Purpose |
|------|---------|---------|
| `config.Save(path, cfg) error` | `internal/config` | Persist config changes (profiles, registries) |
| `Vacuum(ctx) error` | `internal/storage` | SQLite VACUUM |
| `Reindex(ctx) error` | `internal/storage` | Rebuild FTS indexes |

## Output Format

All commands default to human-readable text tables. A `--json` persistent flag on root enables JSON output. The `printTable` helper uses `text/tabwriter` for alignment. The `outputJSON` helper uses `json.MarshalIndent`.

## Implementation Order

1. Shared infrastructure (`helpers.go`, `--json` flag)
2. New service methods (CancelJob, SearchEntities, Compose)
3. New config/storage methods (Save, Vacuum, Reindex)
4. Config commands (simplest, validates helpers)
5. Core CRUD (open, list, delete, edit)
6. Entities + Find
7. Jobs
8. Profiles
9. Registry
10. Housekeeping
11. Make

# US-0044: Implement Registry Adapter Plugin

**System Types:** dpkms (self-hosted), dpkms cloud
**Personas:** [Platform Integrators](../../personas/platform-integrators.md)

---

## User Goal

As a platform integrator, I want to implement a registry adapter plugin so that I can
resolve human-readable aliases (e.g., `my-project`, `sprint-42`) to knowledge object IDs,
enabling the rest of the system to reference objects by name rather than UUID.

---

## Context

The `pluginapi.AliasResolver` interface extends `pluginapi.Plugin` with:

```go
type AliasResolver interface {
    Plugin
    ResolveID(ctx context.Context, idOrAlias, profile string) (string, error)
}
```

The plugin registry (`internal/plugin.Registry`) iterates `AliasResolvers()` in registration
order on each lookup. The first resolver that returns a non-empty, changed ID wins; others are
skipped. If no resolver matches, the input is returned unchanged.

Aliases are persisted in the `aliases` table via `pluginapi.AliasStore` (injected as
`deps.Store.Aliases()`). The built-in `aliasing` plugin in `plugins/aliasing/` is the
reference implementation; custom registry adapters follow the same contract.

Aliases carry a `scope` (`global` | `profile`) and an optional `profile` string for
profile-scoped lookups.

---

## Acceptance Criteria

- **Plugin satisfies `pluginapi.AliasResolver`** — `ResolveID` implemented in addition to
  base `Plugin` methods
- **`Init` accesses alias store** via `deps.Store.Aliases()` without error
- **Alias creation persisted** — calling the plugin's create alias operation writes a row to
  the `aliases` table with correct `alias`, `object_id`, `scope`, `profile` values
- **Alias resolution returns canonical ID** — `ResolveID("my-alias", "")` returns the stored
  `object_id`; unknown aliases return input unchanged (no error)
- **Profile-scoped resolution** — profile-scoped alias resolves correctly when `profile`
  matches; does not resolve under a different profile
- **Global scope resolution** — global alias resolves regardless of `profile` argument
- **Alias deletion** — after deletion, `ResolveID` returns the original alias unchanged
- **Duplicate alias rejected** — creating an alias with an existing `(alias, scope, profile)`
  key returns an error
- **Plugin listed in registry** — after server start, `AliasResolvers()` includes this plugin

---

## Implementation Notes

### Plugin Structure

```go
type MyRegistryPlugin struct {
    store pluginapi.AliasStore
}

func (p *MyRegistryPlugin) Name() string    { return "my-registry" }
func (p *MyRegistryPlugin) Version() string { return "1.0.0" }

func (p *MyRegistryPlugin) Init(
    _ context.Context, _ map[string]interface{}, deps pluginapi.Deps,
) error {
    if deps.Store == nil {
        return fmt.Errorf("my-registry: store not injected")
    }
    p.store = deps.Store.Aliases()
    return nil
}

func (p *MyRegistryPlugin) PipelineSteps() []pluginapi.PipelineStep { return nil }
func (p *MyRegistryPlugin) Close(_ context.Context) error           { return nil }

// ResolveID implements pluginapi.AliasResolver.
func (p *MyRegistryPlugin) ResolveID(
    ctx context.Context, idOrAlias, profile string,
) (string, error) {
    id, err := p.store.Resolve(ctx, idOrAlias, profile)
    if err != nil {
        return idOrAlias, nil // not an alias; pass through
    }
    return id, nil
}
```

### Alias Storage Schema

```sql
CREATE TABLE IF NOT EXISTS aliases (
    alias      TEXT NOT NULL,
    object_id  TEXT NOT NULL,
    scope      TEXT NOT NULL DEFAULT 'global',
    profile    TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (alias, scope, profile)
);
```

### Alias Store Interface

Operations available via `pluginapi.AliasStore`:

- `Create(ctx, *Alias) error`
- `Resolve(ctx, alias, profile string) (string, error)`
- `List(ctx, AliasFilter) ([]*Alias, error)`
- `Delete(ctx, alias, scope, profile string) error`

### Config Declaration

```yaml
plugins:
  - plugin: my-registry
    config:
      default_scope: global
```

### HTTP Endpoints (server-side, via `handlers_aliases.go`)

- `POST   /api/v1/aliases` — create alias
- `GET    /api/v1/aliases` — list aliases (query param: `object_id`)
- `GET    /api/v1/aliases/{alias}` — resolve alias to object ID (query param: `profile`)
- `DELETE /api/v1/aliases/{alias}` — delete alias (query params: `scope`, `profile`)

---

## E2E Test Checklist

- [ ] Register plugin at startup — server logs show `plugin: my-registry 1.0.0 initialised`
- [ ] `Init` called with nil `deps.Store` → returns descriptive error; server handles
  gracefully (logs and skips or refuses to start per policy)
- [ ] Create alias via plugin `Create` (or POST `/api/v1/aliases`); verify request body
  contains `{"alias":"my-alias","object_id":"<uuid>","scope":"global"}` sent to server
- [ ] Verify row in DB: `SELECT * FROM aliases WHERE alias='my-alias'` — returns row with
  correct `object_id`, `scope='global'`, `profile=''`
- [ ] `ResolveID("my-alias", "")` on the plugin instance → returns stored `object_id`
- [ ] `ResolveID("unknown-alias", "")` → returns `"unknown-alias"` unchanged with no error
- [ ] Create profile-scoped alias `(alias="sprint-42", scope="profile", profile="dev")`;
  `ResolveID("sprint-42", "dev")` → returns `object_id`
- [ ] `ResolveID("sprint-42", "prod")` (different profile) → returns `"sprint-42"` unchanged
- [ ] Create global alias `(alias="shared", scope="global")`;
  `ResolveID("shared", "any-profile")` → returns `object_id` regardless of profile
- [ ] DELETE `/api/v1/aliases/my-alias?scope=global&profile=` → 204 No Content; row removed
  from `aliases` table; subsequent `ResolveID("my-alias", "")` returns `"my-alias"` unchanged
- [ ] Create duplicate alias `(alias="my-alias", scope="global", profile="")` twice →
  second `Create` returns error; only one row in DB
- [ ] GET `/api/v1/aliases?object_id=<uuid>` — response includes the alias row(s) matching
  `object_id`; payload contains `alias`, `scope`, `profile`, `object_id` fields
- [ ] Multiple `AliasResolver` plugins registered in order; first match wins — verify by
  registering two plugins where only the second knows alias "x"; `ResolveID("x")` returns
  correct ID from second plugin
- [ ] Server `SIGTERM` — `Close()` called without error

---

## Related Stories

- [US-0042](./US-0042-implement-custom-enrichment-plugin.md) - Custom enrichment plugin
- [US-0043](./US-0043-implement-custom-ai-provider-plugin.md) - Custom AI provider plugin
- [US-0045](./US-0045-implement-custom-ranking-algorithm.md) - Custom ranking algorithm
- [US-0029](../admin/US-0029-install-and-enable-plugin.md) - Install and enable plugin
- [US-0016](../search/US-0016-natural-language-search.md) - Natural language search (uses
  alias resolution for `@entity` mentions)

---

## Personas

- [Maintainers](../../personas/maintainers.md)
- [Platform Integrators](../../personas/platform-integrators.md)
- [OSS Go Developer](/Users/jadb/.w/ideacrafterslabs/.docs/personas/contributors/oss-go-developer.md)

---

## E2E Tests

> Not yet implemented.

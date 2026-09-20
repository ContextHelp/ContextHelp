# Plugin: Object Aliasing Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Plugin enabling human-readable aliases for knowledge objects (e.g., "project-charter" → obj_abc123). Configurable scope: per-profile, global, or owner. Enables `ctxt open project-charter` instead of `ctxt open obj_abc123`.

**Architecture:** Aliases stored in a new `aliases` table (migration 010). Plugin registers a storage hook that resolves aliases before object lookup. CLI and REST API transparently resolve aliases. Scope: "global" = all profiles; "profile" = only in active profile; "owner" = only creator can use (future). Extraction-ready: all plugin code in `plugins/aliasing/`.

**Tech Stack:** Go, SQLite (new table), existing storage/service pattern.

---

## Plugin System Findings (read before implementing)

### Plugin interface (defined in P500 Task 0)

See `internal/plugin/plugin.go`. The aliasing plugin implements `plugin.Plugin` (Name, Version, Init, PipelineSteps, Close). It does **not** implement `PostIngestHook` — it is purely storage-layer.

### Storage driver (`internal/storage/storage.go`)

`StorageDriver` exposes typed sub-stores via accessor methods (`Objects()`, `Entities()`, etc.). Adding a new sub-store follows this pattern:
1. Define `AliasStore` interface in `internal/storage/storage.go`.
2. Add `Aliases() AliasStore` to `StorageDriver` interface.
3. Implement in the SQLite driver (`internal/storage/sqlite/`).

The SQLite driver is in `internal/storage/sqlite/`. It uses embedded SQL migrations (found in `internal/storage/sqlite/migrations/`). Migrations 001-007 exist; P240 defines 008 and 009 for inbox. This plan uses **migration 010**.

### Config (`internal/config/config.go`)

`PluginConfig.Config map[string]interface{}` is the settings block. Aliasing has minimal config: just `enabled` and `default_scope`.

### Object lookup (resolution transparency)

`internal/service/service.go` exposes `GetObject(ctx, id)` which calls `s.Store.Objects().Get(ctx, id)`. The cleanest hook for alias resolution is a **service-layer wrapper** that calls `AliasStore.ResolveAlias` first and then delegates to `Objects().Get` with the resolved ID. This avoids modifying the `StorageDriver` interface (breaking change) while keeping resolution transparent.

Alternative considered: StorageDriver decorator. Rejected because `StorageDriver` is an interface with 16 methods — wrapping it requires a full delegation shim. Service-layer wrapper is simpler.

### Chosen approach: service-level resolver

Add `AliasResolver` interface to `internal/plugin/plugin.go`:

```go
type AliasResolver interface {
    Plugin
    ResolveID(ctx context.Context, idOrAlias, profile string) (string, error)
}
```

The service checks the plugin registry's alias resolvers before object lookup. One call to `registry.ResolveID(id, profile)` returns the canonical object ID.

### Migration numbering

Existing migrations: 001-007. P240 (inbox) reserves 008 and 009. This plan uses **010**. P520 (audit log) uses **011**.

---

## Task List

| # | Task | File(s) | Est. |
|---|------|---------|------|
| T1 | Migration 010 — aliases table | `internal/storage/sqlite/migrations/010_aliases.sql`, `migrations.go` | 5 min |
| T2 | AliasStore interface + types | `internal/storage/storage.go`, `internal/storage/sqlite/aliases.go` | 15 min |
| T3 | Wire AliasStore into StorageDriver | `internal/storage/sqlite/driver.go` | 10 min |
| T4 | AliasResolver interface + service wiring | `internal/plugin/plugin.go`, `internal/service/service.go` | 10 min |
| T5 | Plugin module scaffold + go.mod | `plugins/aliasing/` | 5 min |
| T6 | AliasConfig | `plugins/aliasing/config.go` | 5 min |
| T7 | AliasStore implementation (SQLite) | `plugins/aliasing/store.go` | 20 min |
| T8 | Resolver logic | `plugins/aliasing/resolver.go` | 10 min |
| T9 | Plugin struct | `plugins/aliasing/plugin.go` | 10 min |
| T10 | REST API handlers | `internal/server/http/handlers_aliases.go` | 15 min |
| T11 | CLI commands | `plugins/aliasing/cli.go` | 15 min |
| T12 | Transparent resolution in `ctxt open` | `cmd/ctxt/cmd/open.go` | 5 min |
| T13 | Integration test | `plugins/aliasing/plugin_test.go` | 15 min |
| T14 | Extraction-readiness verification | `plugins/aliasing/go.mod`, `go.work` | 5 min |

---

## Task 1: Migration 010

**Files:**
- create: `internal/storage/sqlite/migrations/010_aliases.sql`
- edit: `internal/storage/sqlite/migrations.go`

**Step 1.1 — write SQL**

Create `internal/storage/sqlite/migrations/010_aliases.sql`:

```sql
CREATE TABLE IF NOT EXISTS aliases (
    alias       TEXT NOT NULL,
    object_id   TEXT NOT NULL REFERENCES objects(id) ON DELETE CASCADE,
    scope       TEXT NOT NULL DEFAULT 'global',
    profile     TEXT NOT NULL DEFAULT '',
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL,
    PRIMARY KEY (alias, scope, profile)
);

CREATE INDEX IF NOT EXISTS idx_aliases_object_id ON aliases(object_id);
CREATE INDEX IF NOT EXISTS idx_aliases_scope_profile ON aliases(scope, profile);
```

Notes:
- `scope` values: `"global"` (accessible from all profiles) or `"profile"` (scoped to one profile).
- `profile` is the profile name; empty string (`""`) for global-scope aliases.
- `PRIMARY KEY (alias, scope, profile)` allows the same alias name in different scopes/profiles without collision.
- `ON DELETE CASCADE` removes aliases when the referenced object is deleted.

**Step 1.2 — register embed**

In `internal/storage/sqlite/migrations.go`, after existing embed directives and the migrations slice:

```go
//go:embed migrations/010_aliases.sql
var migration010 string

// Add to the migrations slice:
{Version: 10, SQL: migration010},
```

(Follow the exact pattern of existing migrations in that file.)

**Step 1.3 — write test**

In `internal/storage/sqlite/` write a test (or extend existing migration test):

```go
func TestMigration010_AliasesTable(t *testing.T) {
    db := setupTestDB(t)
    // Table must exist after migration.
    _, err := db.ExecContext(context.Background(),
        `INSERT INTO aliases (alias, object_id, scope, profile, created_at, updated_at)
         VALUES ('test-alias', 'fake-id', 'global', '', datetime('now'), datetime('now'))`)
    // Expect foreign key failure (object does not exist), not "no such table".
    require.Error(t, err)
    require.Contains(t, err.Error(), "FOREIGN KEY constraint failed")
}
```

**Step 1.4 — run test, commit**

```bash
go test ./internal/storage/sqlite/... -run TestMigration010
```

Expected: `PASS` (the test validates the table exists by triggering the FK constraint, not a "no such table" error).

```
git add internal/storage/sqlite/migrations/010_aliases.sql internal/storage/sqlite/migrations.go
git commit -m "feat(storage): add migration 010 — aliases table"
```

---

## Task 2: AliasStore Interface + Types

**Files:**
- edit: `internal/storage/storage.go`
- create: `internal/storage/sqlite/aliases.go`
- create: `internal/storage/sqlite/aliases_test.go`

**Step 2.1 — add types and interface to storage.go**

In `internal/storage/storage.go`, add:

```go
// Alias represents a human-readable name that resolves to a knowledge object ID.
type Alias struct {
    Alias     string    `json:"alias"`
    ObjectID  string    `json:"object_id"`
    Scope     string    `json:"scope"`   // "global" | "profile"
    Profile   string    `json:"profile"` // empty for global
    CreatedAt time.Time `json:"created_at"`
    UpdatedAt time.Time `json:"updated_at"`
}

// AliasFilter restricts alias listing results.
type AliasFilter struct {
    ObjectID string
    Scope    string
    Profile  string
}

// AliasStore persists and retrieves object aliases.
type AliasStore interface {
    // Create stores a new alias. Returns error if the alias+scope+profile triple already exists.
    Create(ctx context.Context, a *Alias) error
    // Resolve returns the object ID for the given alias, considering global scope and
    // the provided profile scope. Returns ("", ErrNotFound) if no match.
    Resolve(ctx context.Context, alias, profile string) (string, error)
    // List returns all aliases matching the filter.
    List(ctx context.Context, filter AliasFilter) ([]*Alias, error)
    // Delete removes the alias with the given alias+scope+profile triple.
    Delete(ctx context.Context, alias, scope, profile string) error
}
```

Also add `Aliases() AliasStore` to the `StorageDriver` interface.

**Step 2.2 — write test first**

Create `internal/storage/sqlite/aliases_test.go`:

```go
package sqlite_test

import (
    "context"
    "testing"
    "time"

    "github.com/ideacrafterslabs/ctxt/internal/storage"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func TestAliasStore_CreateAndResolve(t *testing.T) {
    db := newTestDriver(t)
    ctx := context.Background()

    // Create a real object first.
    obj := testObject(t)
    require.NoError(t, db.Objects().Create(ctx, obj))

    now := time.Now().UTC().Truncate(time.Second)
    a := &storage.Alias{
        Alias:     "my-doc",
        ObjectID:  obj.ID,
        Scope:     "global",
        Profile:   "",
        CreatedAt: now,
        UpdatedAt: now,
    }
    require.NoError(t, db.Aliases().Create(ctx, a))

    // Resolve: global scope should work regardless of profile.
    id, err := db.Aliases().Resolve(ctx, "my-doc", "any-profile")
    require.NoError(t, err)
    assert.Equal(t, obj.ID, id)
}

func TestAliasStore_ProfileScope_DoesNotLeakToOtherProfile(t *testing.T) {
    db := newTestDriver(t)
    ctx := context.Background()

    obj := testObject(t)
    require.NoError(t, db.Objects().Create(ctx, obj))

    now := time.Now().UTC().Truncate(time.Second)
    a := &storage.Alias{
        Alias:     "private-doc",
        ObjectID:  obj.ID,
        Scope:     "profile",
        Profile:   "alice",
        CreatedAt: now,
        UpdatedAt: now,
    }
    require.NoError(t, db.Aliases().Create(ctx, a))

    // Alice can resolve.
    id, err := db.Aliases().Resolve(ctx, "private-doc", "alice")
    require.NoError(t, err)
    assert.Equal(t, obj.ID, id)

    // Bob cannot resolve.
    _, err = db.Aliases().Resolve(ctx, "private-doc", "bob")
    require.Error(t, err)
}

func TestAliasStore_List(t *testing.T) {
    db := newTestDriver(t)
    ctx := context.Background()

    obj := testObject(t)
    require.NoError(t, db.Objects().Create(ctx, obj))

    now := time.Now().UTC().Truncate(time.Second)
    for _, alias := range []string{"alias-a", "alias-b"} {
        require.NoError(t, db.Aliases().Create(ctx, &storage.Alias{
            Alias: alias, ObjectID: obj.ID, Scope: "global",
            Profile: "", CreatedAt: now, UpdatedAt: now,
        }))
    }

    aliases, err := db.Aliases().List(ctx, storage.AliasFilter{ObjectID: obj.ID})
    require.NoError(t, err)
    assert.Len(t, aliases, 2)
}

func TestAliasStore_Delete(t *testing.T) {
    db := newTestDriver(t)
    ctx := context.Background()

    obj := testObject(t)
    require.NoError(t, db.Objects().Create(ctx, obj))

    now := time.Now().UTC().Truncate(time.Second)
    require.NoError(t, db.Aliases().Create(ctx, &storage.Alias{
        Alias: "to-delete", ObjectID: obj.ID, Scope: "global",
        Profile: "", CreatedAt: now, UpdatedAt: now,
    }))
    require.NoError(t, db.Aliases().Delete(ctx, "to-delete", "global", ""))

    _, err := db.Aliases().Resolve(ctx, "to-delete", "")
    require.Error(t, err, "alias should be gone after delete")
}
```

**Step 2.3 — write implementation**

Create `internal/storage/sqlite/aliases.go`:

```go
package sqlite

import (
    "context"
    "database/sql"
    "errors"
    "fmt"
    "time"

    "github.com/ideacrafterslabs/ctxt/internal/storage"
)

type aliasStore struct{ db *sql.DB }

func (s *aliasStore) Create(ctx context.Context, a *storage.Alias) error {
    _, err := s.db.ExecContext(ctx,
        `INSERT INTO aliases (alias, object_id, scope, profile, created_at, updated_at)
         VALUES (?, ?, ?, ?, ?, ?)`,
        a.Alias, a.ObjectID, a.Scope, a.Profile,
        a.CreatedAt.UTC().Format(time.RFC3339),
        a.UpdatedAt.UTC().Format(time.RFC3339),
    )
    if err != nil {
        return fmt.Errorf("alias create: %w", err)
    }
    return nil
}

// Resolve checks profile-scope first, then global scope.
func (s *aliasStore) Resolve(ctx context.Context, alias, profile string) (string, error) {
    // Profile-scoped lookup first (if profile is non-empty).
    if profile != "" {
        var id string
        err := s.db.QueryRowContext(ctx,
            `SELECT object_id FROM aliases WHERE alias=? AND scope='profile' AND profile=?`,
            alias, profile,
        ).Scan(&id)
        if err == nil {
            return id, nil
        }
        if !errors.Is(err, sql.ErrNoRows) {
            return "", err
        }
    }
    // Fall through to global lookup.
    var id string
    err := s.db.QueryRowContext(ctx,
        `SELECT object_id FROM aliases WHERE alias=? AND scope='global'`,
        alias,
    ).Scan(&id)
    if errors.Is(err, sql.ErrNoRows) {
        return "", fmt.Errorf("alias %q not found", alias)
    }
    return id, err
}

func (s *aliasStore) List(ctx context.Context, filter storage.AliasFilter) ([]*storage.Alias, error) {
    query := `SELECT alias, object_id, scope, profile, created_at, updated_at FROM aliases WHERE 1=1`
    var args []interface{}
    if filter.ObjectID != "" {
        query += ` AND object_id=?`
        args = append(args, filter.ObjectID)
    }
    if filter.Scope != "" {
        query += ` AND scope=?`
        args = append(args, filter.Scope)
    }
    if filter.Profile != "" {
        query += ` AND profile=?`
        args = append(args, filter.Profile)
    }
    rows, err := s.db.QueryContext(ctx, query, args...)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var out []*storage.Alias
    for rows.Next() {
        var a storage.Alias
        var createdStr, updatedStr string
        if err := rows.Scan(&a.Alias, &a.ObjectID, &a.Scope, &a.Profile, &createdStr, &updatedStr); err != nil {
            return nil, err
        }
        a.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
        a.UpdatedAt, _ = time.Parse(time.RFC3339, updatedStr)
        out = append(out, &a)
    }
    return out, rows.Err()
}

func (s *aliasStore) Delete(ctx context.Context, alias, scope, profile string) error {
    _, err := s.db.ExecContext(ctx,
        `DELETE FROM aliases WHERE alias=? AND scope=? AND profile=?`,
        alias, scope, profile,
    )
    return err
}
```

**Step 2.4 — run tests, commit**

```bash
go test ./internal/storage/sqlite/... -run TestAliasStore
```

Expected: `PASS`.

```
git add internal/storage/storage.go internal/storage/sqlite/aliases.go internal/storage/sqlite/aliases_test.go
git commit -m "feat(storage): add AliasStore interface and SQLite implementation"
```

---

## Task 3: Wire AliasStore into StorageDriver

**Files:**
- edit: `internal/storage/sqlite/driver.go` (the concrete SQLite StorageDriver)

**Step 3.1 — add Aliases() method**

Locate the SQLite `Driver` struct and add:

```go
func (d *Driver) Aliases() storage.AliasStore {
    return &aliasStore{db: d.db}
}
```

Also add `Aliases() storage.AliasStore` to any mock `StorageDriver` used in tests (if there is one in `test/` or `internal/`).

**Step 3.2 — compile check**

```bash
go build ./...
```

Expected: no errors.

**Step 3.3 — commit**

```
git add internal/storage/sqlite/driver.go
git commit -m "feat(storage/sqlite): wire AliasStore into StorageDriver"
```

---

## Task 4: AliasResolver Interface + Service Wiring

**Files:**
- edit: `internal/plugin/plugin.go`
- edit: `internal/service/service.go`

**Step 4.1 — add AliasResolver to plugin.go**

In `internal/plugin/plugin.go`, add:

```go
// AliasResolver is implemented by plugins that can resolve aliases to object IDs.
type AliasResolver interface {
    Plugin
    // ResolveID resolves an alias (or passes through an ID unchanged).
    // Returns the canonical object ID or the input unchanged if not an alias.
    ResolveID(ctx context.Context, idOrAlias, profile string) (string, error)
}
```

Add to `Registry`:

```go
// AliasResolvers returns all plugins implementing AliasResolver.
func (r *Registry) AliasResolvers() []AliasResolver {
    var out []AliasResolver
    for _, p := range r.plugins {
        if ar, ok := p.(AliasResolver); ok {
            out = append(out, ar)
        }
    }
    return out
}

// ResolveID tries each registered AliasResolver in order, returning on first success.
// Falls back to returning the input unchanged.
func (r *Registry) ResolveID(ctx context.Context, idOrAlias, profile string) (string, error) {
    for _, ar := range r.AliasResolvers() {
        id, err := ar.ResolveID(ctx, idOrAlias, profile)
        if err == nil && id != "" && id != idOrAlias {
            return id, nil
        }
    }
    return idOrAlias, nil
}
```

**Step 4.2 — wire into Service**

Add `PluginRegistry *plugin.Registry` field to `internal/service/service.go`'s `Service` struct:

```go
type Service struct {
    Store          storage.StorageDriver
    Queue          *jobs.Queue
    Pipes          pipeline.Registry
    Search         *search.Engine
    Discovery      *steps.StepDiscovery
    Executor       *steps.StepExecutor
    Bus            events.Bus
    PluginRegistry *plugin.Registry  // NEW
}
```

Modify `GetObject` to resolve aliases:

```go
func (s *Service) GetObject(ctx context.Context, idOrAlias string) (*storage.KnowledgeObject, error) {
    id := idOrAlias
    if s.PluginRegistry != nil {
        // TODO: thread active profile through context or accept as parameter.
        // For now, use empty profile (resolves global aliases).
        resolved, err := s.PluginRegistry.ResolveID(ctx, idOrAlias, "")
        if err == nil {
            id = resolved
        }
    }
    return s.Store.Objects().Get(ctx, id)
}
```

**Step 4.3 — write test**

In `internal/service/service_test.go` (or a new file), add:

```go
func TestService_GetObject_ResolvesAlias(t *testing.T) {
    // Uses a stub plugin registry with a mock resolver.
    // Verify that GetObject("my-alias") calls Resolve and returns the correct object.
}
```

**Step 4.4 — compile, test, commit**

```bash
go build ./... && go test ./internal/service/... -run TestService_GetObject
```

```
git add internal/plugin/plugin.go internal/service/service.go
git commit -m "feat(plugin): add AliasResolver interface and service-level resolution"
```

---

## Task 5: Plugin Module Scaffold + go.mod

**Files:**
- create: `plugins/aliasing/go.mod`

**Step 5.1 — create directory and go.mod**

```bash
mkdir -p plugins/aliasing
```

Create `plugins/aliasing/go.mod`:

```go
module github.com/ideacrafterslabs/ctxt-plugin-aliasing

go 1.25.6

require (
    github.com/google/uuid v1.6.0
    github.com/ideacrafterslabs/ctxt v0.0.0
    github.com/spf13/cobra v1.10.2
    github.com/stretchr/testify v1.11.1
)

replace github.com/ideacrafterslabs/ctxt => ../..
```

**Step 5.2 — add to go.work**

Edit `./go.work`:

```
go 1.26.1

use .
use ./plugins/autosuggest
use ./plugins/aliasing
```

**Step 5.3 — commit**

```
git add plugins/aliasing/go.mod go.work
git commit -m "feat(plugin/aliasing): scaffold module"
```

---

## Task 6: AliasConfig

**Files:**
- create: `plugins/aliasing/config.go`
- create: `plugins/aliasing/config_test.go`

**Step 6.1 — write test**

Create `plugins/aliasing/config_test.go`:

```go
package aliasing_test

import (
    "testing"

    aliasing "github.com/ideacrafterslabs/ctxt-plugin-aliasing"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func TestAliasConfig_Defaults(t *testing.T) {
    cfg := aliasing.DefaultConfig()
    assert.True(t, cfg.Enabled)
    assert.Equal(t, "global", cfg.DefaultScope)
}

func TestAliasConfig_FromMap(t *testing.T) {
    raw := map[string]interface{}{
        "enabled":       false,
        "default_scope": "profile",
    }
    cfg, err := aliasing.ConfigFromMap(raw)
    require.NoError(t, err)
    assert.False(t, cfg.Enabled)
    assert.Equal(t, "profile", cfg.DefaultScope)
}
```

**Step 6.2 — write config.go**

Create `plugins/aliasing/config.go`:

```go
package aliasing

import "fmt"

// AliasConfig controls plugin behaviour.
type AliasConfig struct {
    // Enabled controls whether the plugin is active (default true).
    Enabled bool
    // DefaultScope is the scope used when none is specified in CLI/API: "global" or "profile".
    DefaultScope string
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() AliasConfig {
    return AliasConfig{
        Enabled:      true,
        DefaultScope: "global",
    }
}

// ConfigFromMap decodes a raw config map into AliasConfig.
func ConfigFromMap(m map[string]interface{}) (AliasConfig, error) {
    cfg := DefaultConfig()
    if m == nil {
        return cfg, nil
    }
    if v, ok := m["enabled"].(bool); ok {
        cfg.Enabled = v
    }
    if v, ok := m["default_scope"].(string); ok {
        if v != "global" && v != "profile" {
            return cfg, fmt.Errorf("aliasing: default_scope must be 'global' or 'profile', got %q", v)
        }
        cfg.DefaultScope = v
    }
    return cfg, nil
}
```

**Step 6.3 — run test, commit**

```bash
cd plugins/aliasing && go test ./... -run TestAliasConfig
```

Expected: `PASS`.

```
git add plugins/aliasing/config.go plugins/aliasing/config_test.go
git commit -m "feat(plugin/aliasing): add AliasConfig"
```

---

## Task 7: AliasStore Usage in Plugin

**Files:**
- create: `plugins/aliasing/store.go`
- create: `plugins/aliasing/store_test.go`

The plugin delegates to `storage.AliasStore` (already implemented in the core SQLite driver). This file provides helpers and a thin wrapper used by the plugin.

**Step 7.1 — write store_test.go**

Create `plugins/aliasing/store_test.go`:

```go
package aliasing_test

import (
    "context"
    "testing"
    "time"

    aliasing "github.com/ideacrafterslabs/ctxt-plugin-aliasing"
    "github.com/ideacrafterslabs/ctxt/internal/storage"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

// mockAliasStore is an in-memory implementation for tests.
type mockAliasStore struct {
    aliases []*storage.Alias
}

func (m *mockAliasStore) Create(_ context.Context, a *storage.Alias) error {
    m.aliases = append(m.aliases, a)
    return nil
}

func (m *mockAliasStore) Resolve(_ context.Context, alias, profile string) (string, error) {
    // Profile-scope first, then global.
    for _, a := range m.aliases {
        if a.Alias == alias && a.Scope == "profile" && a.Profile == profile {
            return a.ObjectID, nil
        }
    }
    for _, a := range m.aliases {
        if a.Alias == alias && a.Scope == "global" {
            return a.ObjectID, nil
        }
    }
    return "", fmt.Errorf("not found")
}

func (m *mockAliasStore) List(_ context.Context, f storage.AliasFilter) ([]*storage.Alias, error) {
    var out []*storage.Alias
    for _, a := range m.aliases {
        if f.ObjectID != "" && a.ObjectID != f.ObjectID {
            continue
        }
        out = append(out, a)
    }
    return out, nil
}

func (m *mockAliasStore) Delete(_ context.Context, alias, scope, profile string) error {
    var filtered []*storage.Alias
    for _, a := range m.aliases {
        if a.Alias != alias || a.Scope != scope || a.Profile != profile {
            filtered = append(filtered, a)
        }
    }
    m.aliases = filtered
    return nil
}

func TestSetAlias_CreatesRecord(t *testing.T) {
    store := &mockAliasStore{}
    now := time.Now()
    err := aliasing.SetAlias(context.Background(), store, "my-doc", "obj_001", "global", "", now)
    require.NoError(t, err)
    aliases, err := store.List(context.Background(), storage.AliasFilter{ObjectID: "obj_001"})
    require.NoError(t, err)
    require.Len(t, aliases, 1)
    assert.Equal(t, "my-doc", aliases[0].Alias)
}
```

**Step 7.2 — write store.go**

Create `plugins/aliasing/store.go`:

```go
package aliasing

import (
    "context"
    "time"

    "github.com/ideacrafterslabs/ctxt/internal/storage"
)

// SetAlias creates a new alias record.
func SetAlias(ctx context.Context, store storage.AliasStore, alias, objectID, scope, profile string, now time.Time) error {
    return store.Create(ctx, &storage.Alias{
        Alias:     alias,
        ObjectID:  objectID,
        Scope:     scope,
        Profile:   profile,
        CreatedAt: now,
        UpdatedAt: now,
    })
}

// ResolveAlias resolves an alias to an object ID.
// Returns (idOrAlias, nil) unchanged if the alias is not found (soft resolution).
func ResolveAlias(ctx context.Context, store storage.AliasStore, idOrAlias, profile string) (string, error) {
    id, err := store.Resolve(ctx, idOrAlias, profile)
    if err != nil {
        // Not found: return input unchanged.
        return idOrAlias, nil
    }
    return id, nil
}
```

**Step 7.3 — run test, commit**

```bash
cd plugins/aliasing && go test ./... -run TestSetAlias
```

Expected: `PASS`.

```
git add plugins/aliasing/store.go plugins/aliasing/store_test.go
git commit -m "feat(plugin/aliasing): add store helpers and mock"
```

---

## Task 8: Resolver Logic

**Files:**
- create: `plugins/aliasing/resolver.go`
- create: `plugins/aliasing/resolver_test.go`

**Step 8.1 — write test**

Create `plugins/aliasing/resolver_test.go`:

```go
package aliasing_test

import (
    "context"
    "testing"
    "time"

    aliasing "github.com/ideacrafterslabs/ctxt-plugin-aliasing"
    "github.com/ideacrafterslabs/ctxt/internal/storage"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func TestResolveID_KnownAlias_ReturnsObjectID(t *testing.T) {
    store := &mockAliasStore{}
    now := time.Now()
    _ = aliasing.SetAlias(context.Background(), store, "charter", "obj_abc", "global", "", now)

    id, err := aliasing.ResolveAlias(context.Background(), store, "charter", "")
    require.NoError(t, err)
    assert.Equal(t, "obj_abc", id)
}

func TestResolveID_UnknownAlias_ReturnsSelf(t *testing.T) {
    store := &mockAliasStore{}
    id, err := aliasing.ResolveAlias(context.Background(), store, "obj_abc123", "")
    require.NoError(t, err)
    assert.Equal(t, "obj_abc123", id, "unknown alias passes through unchanged")
}
```

**Step 8.2 — (no new code needed; logic is in store.go ResolveAlias)**

The resolver tests already exercise `store.go`. Add documentation to `resolver.go` as a dedicated file for clarity:

Create `plugins/aliasing/resolver.go`:

```go
package aliasing

// resolver.go documents the alias resolution strategy.
//
// Resolution order (for a given alias + profile):
//   1. Profile-scoped alias (scope="profile", profile=<activeProfile>)
//   2. Global alias (scope="global")
//   3. Input returned unchanged (treated as a direct object ID)
//
// This means a profile-scoped alias shadows a global alias of the same name.
// The service layer calls ResolveAlias before every object lookup, making
// alias resolution fully transparent to callers.
```

**Step 8.3 — run test, commit**

```bash
cd plugins/aliasing && go test ./... -run TestResolveID
```

Expected: `PASS`.

```
git add plugins/aliasing/resolver.go plugins/aliasing/resolver_test.go
git commit -m "feat(plugin/aliasing): add resolver logic and tests"
```

---

## Task 9: Plugin Struct

**Files:**
- create: `plugins/aliasing/plugin.go`

**Step 9.1 — write plugin.go**

Create `plugins/aliasing/plugin.go`:

```go
// Package aliasing provides a plugin for human-readable object aliases.
package aliasing

import (
    "context"
    "fmt"

    "github.com/ideacrafterslabs/ctxt/internal/pipeline"
    "github.com/ideacrafterslabs/ctxt/internal/plugin"
    "github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Plugin is the plugin.Plugin + plugin.AliasResolver implementation.
type Plugin struct {
    cfg   AliasConfig
    store storage.AliasStore
}

// New returns an uninitialised Plugin for registration.
func New() *Plugin { return &Plugin{} }

func (p *Plugin) Name() string    { return "aliasing" }
func (p *Plugin) Version() string { return "1.0.0" }

func (p *Plugin) Init(ctx context.Context, raw map[string]interface{}, deps plugin.Deps) error {
    cfg, err := ConfigFromMap(raw)
    if err != nil {
        return fmt.Errorf("aliasing: config: %w", err)
    }
    p.cfg = cfg
    if deps.Store != nil {
        p.store = deps.Store.Aliases()
    }
    return nil
}

// PipelineSteps returns nothing — aliasing is not a pipeline step.
func (p *Plugin) PipelineSteps() []pipeline.PipelineStep { return nil }

func (p *Plugin) Close(_ context.Context) error { return nil }

// ResolveID implements plugin.AliasResolver.
func (p *Plugin) ResolveID(ctx context.Context, idOrAlias, profile string) (string, error) {
    if !p.cfg.Enabled || p.store == nil {
        return idOrAlias, nil
    }
    return ResolveAlias(ctx, p.store, idOrAlias, profile)
}

// SetAlias creates an alias via the plugin (called by REST/CLI handlers).
func (p *Plugin) SetAlias(ctx context.Context, alias, objectID, scope, profile string) error {
    if p.store == nil {
        return fmt.Errorf("aliasing: store not initialised")
    }
    if scope == "" {
        scope = p.cfg.DefaultScope
    }
    return store_SetAlias(ctx, p.store, alias, objectID, scope, profile, timeNow())
}

// ListAliases lists aliases matching the filter.
func (p *Plugin) ListAliases(ctx context.Context, objectID string) ([]*storage.Alias, error) {
    if p.store == nil {
        return nil, nil
    }
    return p.store.List(ctx, storage.AliasFilter{ObjectID: objectID})
}

// RemoveAlias deletes an alias.
func (p *Plugin) RemoveAlias(ctx context.Context, alias, scope, profile string) error {
    if p.store == nil {
        return fmt.Errorf("aliasing: store not initialised")
    }
    return p.store.Delete(ctx, alias, scope, profile)
}
```

Note: `store_SetAlias` is an alias for `SetAlias` from `store.go` to avoid name collision; rename accordingly, or use a package-level variable `timeNow = time.Now` for testability.

**Step 9.2 — commit**

```
git add plugins/aliasing/plugin.go
git commit -m "feat(plugin/aliasing): add Plugin struct implementing AliasResolver"
```

---

## Task 10: REST API Handlers

**Files:**
- create: `internal/server/http/handlers_aliases.go`
- create: `internal/server/http/handlers_aliases_test.go`

**Step 10.1 — write handlers**

Create `internal/server/http/handlers_aliases.go`:

```go
package http

import (
    "encoding/json"
    "net/http"
    "time"

    "github.com/go-chi/chi/v5"
    "github.com/ideacrafterslabs/ctxt/internal/storage"
)

// aliasPlugin is the minimal interface the handlers need from the aliasing plugin.
// Avoids importing the plugin module directly from the HTTP package.
type aliasPlugin interface {
    SetAlias(ctx context.Context, alias, objectID, scope, profile string) error
    ListAliases(ctx context.Context, objectID string) ([]*storage.Alias, error)
    RemoveAlias(ctx context.Context, alias, scope, profile string) error
    ResolveID(ctx context.Context, idOrAlias, profile string) (string, error)
}

// POST /api/v1/aliases
func (s *Server) handleCreateAlias(w http.ResponseWriter, r *http.Request) {
    ctx := r.Context()
    var req struct {
        Alias    string `json:"alias"`
        ObjectID string `json:"object_id"`
        Scope    string `json:"scope"`
        Profile  string `json:"profile"`
    }
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        writeError(w, http.StatusBadRequest, "invalid JSON")
        return
    }
    if req.Alias == "" || req.ObjectID == "" {
        writeError(w, http.StatusBadRequest, "alias and object_id are required")
        return
    }
    if req.Scope == "" {
        req.Scope = "global"
    }
    if ap := s.aliasPlugin(); ap != nil {
        if err := ap.SetAlias(ctx, req.Alias, req.ObjectID, req.Scope, req.Profile); err != nil {
            writeError(w, http.StatusInternalServerError, err.Error())
            return
        }
    }
    writeJSON(w, http.StatusCreated, map[string]string{"alias": req.Alias, "object_id": req.ObjectID})
}

// GET /api/v1/aliases
func (s *Server) handleListAliases(w http.ResponseWriter, r *http.Request) {
    ctx := r.Context()
    objectID := r.URL.Query().Get("object_id")
    if ap := s.aliasPlugin(); ap != nil {
        aliases, err := ap.ListAliases(ctx, objectID)
        if err != nil {
            writeError(w, http.StatusInternalServerError, err.Error())
            return
        }
        writeJSON(w, http.StatusOK, aliases)
        return
    }
    writeJSON(w, http.StatusOK, []*storage.Alias{})
}

// GET /api/v1/aliases/{alias}
func (s *Server) handleResolveAlias(w http.ResponseWriter, r *http.Request) {
    ctx := r.Context()
    alias := chi.URLParam(r, "alias")
    profile := r.URL.Query().Get("profile")
    if ap := s.aliasPlugin(); ap != nil {
        id, err := ap.ResolveID(ctx, alias, profile)
        if err != nil || id == alias {
            writeError(w, http.StatusNotFound, "alias not found")
            return
        }
        writeJSON(w, http.StatusOK, map[string]string{"alias": alias, "object_id": id})
        return
    }
    writeError(w, http.StatusNotFound, "aliasing plugin not configured")
}

// DELETE /api/v1/aliases/{alias}
func (s *Server) handleDeleteAlias(w http.ResponseWriter, r *http.Request) {
    ctx := r.Context()
    alias := chi.URLParam(r, "alias")
    scope := r.URL.Query().Get("scope")
    profile := r.URL.Query().Get("profile")
    if scope == "" {
        scope = "global"
    }
    if ap := s.aliasPlugin(); ap != nil {
        if err := ap.RemoveAlias(ctx, alias, scope, profile); err != nil {
            writeError(w, http.StatusInternalServerError, err.Error())
            return
        }
    }
    w.WriteHeader(http.StatusNoContent)
}
```

Add to server route registration in `server.go`:

```go
r.Post("/api/v1/aliases", s.handleCreateAlias)
r.Get("/api/v1/aliases", s.handleListAliases)
r.Get("/api/v1/aliases/{alias}", s.handleResolveAlias)
r.Delete("/api/v1/aliases/{alias}", s.handleDeleteAlias)
```

Add `aliasPlugin()` accessor to the `Server` struct (returns the plugin from the registry if configured):

```go
func (s *Server) aliasPlugin() aliasPlugin {
    if s.svc.PluginRegistry == nil {
        return nil
    }
    for _, ar := range s.svc.PluginRegistry.AliasResolvers() {
        if ap, ok := ar.(aliasPlugin); ok {
            return ap
        }
    }
    return nil
}
```

**Step 10.2 — write handler tests**

Create `internal/server/http/handlers_aliases_test.go`. Follow the pattern of `handlers_objects_test.go`. Use a mock `aliasPlugin` implementation.

**Step 10.3 — run tests, commit**

```bash
go test ./internal/server/http/... -run TestAlias
```

```
git add internal/server/http/handlers_aliases.go internal/server/http/server.go
git commit -m "feat(api): add alias CRUD endpoints"
```

---

## Task 11: CLI Commands

**Files:**
- create: `plugins/aliasing/cli.go`

**Step 11.1 — write cli.go**

Create `plugins/aliasing/cli.go`:

```go
package aliasing

import (
    "encoding/json"
    "fmt"
    "net/http"
    "net/url"
    "os"

    "github.com/spf13/cobra"
)

// NewCLICommands returns a cobra.Command subtree for the aliasing plugin.
// Mount it as: rootCmd.AddCommand(aliasing.NewCLICommands(baseURL))
func NewCLICommands(baseURL string) *cobra.Command {
    cmd := &cobra.Command{
        Use:   "alias",
        Short: "Manage human-readable aliases for knowledge objects",
    }

    var scope, profile string

    setCmd := &cobra.Command{
        Use:   "set <alias> <object-id>",
        Short: "Create an alias for an object",
        Args:  cobra.ExactArgs(2),
        RunE: func(cmd *cobra.Command, args []string) error {
            body, _ := json.Marshal(map[string]string{
                "alias":     args[0],
                "object_id": args[1],
                "scope":     scope,
                "profile":   profile,
            })
            resp, err := http.Post(baseURL+"/api/v1/aliases", "application/json",
                bytes.NewReader(body))
            if err != nil {
                return err
            }
            defer resp.Body.Close()
            fmt.Fprintf(os.Stdout, "alias %q → %s\n", args[0], args[1])
            return nil
        },
    }
    setCmd.Flags().StringVar(&scope, "scope", "global", "Scope: global or profile")
    setCmd.Flags().StringVar(&profile, "profile", "", "Profile name (for profile scope)")

    listCmd := &cobra.Command{
        Use:   "list",
        Short: "List aliases",
        RunE: func(cmd *cobra.Command, args []string) error {
            objectID, _ := cmd.Flags().GetString("object")
            q := url.Values{}
            if objectID != "" {
                q.Set("object_id", objectID)
            }
            resp, err := http.Get(baseURL + "/api/v1/aliases?" + q.Encode())
            if err != nil {
                return err
            }
            defer resp.Body.Close()
            var items []map[string]interface{}
            json.NewDecoder(resp.Body).Decode(&items)
            for _, item := range items {
                fmt.Fprintf(os.Stdout, "%s → %s (scope=%s)\n",
                    item["alias"], item["object_id"], item["scope"])
            }
            return nil
        },
    }
    listCmd.Flags().String("object", "", "Filter by object ID")

    removeCmd := &cobra.Command{
        Use:   "remove <alias>",
        Short: "Delete an alias",
        Args:  cobra.ExactArgs(1),
        RunE: func(cmd *cobra.Command, args []string) error {
            q := url.Values{}
            q.Set("scope", scope)
            if profile != "" {
                q.Set("profile", profile)
            }
            req, _ := http.NewRequest(http.MethodDelete,
                baseURL+"/api/v1/aliases/"+args[0]+"?"+q.Encode(), nil)
            client := &http.Client{}
            resp, err := client.Do(req)
            if err != nil {
                return err
            }
            defer resp.Body.Close()
            fmt.Fprintf(os.Stdout, "removed alias %q\n", args[0])
            return nil
        },
    }
    removeCmd.Flags().StringVar(&scope, "scope", "global", "Scope of the alias to remove")
    removeCmd.Flags().StringVar(&profile, "profile", "", "Profile (for profile-scope aliases)")

    cmd.AddCommand(setCmd, listCmd, removeCmd)
    return cmd
}
```

**Step 11.2 — commit**

```
git add plugins/aliasing/cli.go
git commit -m "feat(plugin/aliasing): add CLI alias set/list/remove"
```

---

## Task 12: Transparent Resolution in `ctxt open`

**Files:**
- edit: `cmd/ctxt/cmd/open.go` (or whichever file implements the `ctxt open` command)

**Step 12.1 — locate file**

```bash
grep -r "\"open\"\|UseE.*open\|ctxt open" ./cmd/ --include="*.go" | head -5
```

**Step 12.2 — modify lookup**

Before calling the API to retrieve the object, resolve the argument:

```go
// In the open command's RunE:
objectID := args[0]
// Attempt alias resolution via the REST API before fetching.
if resolved := resolveAliasViaAPI(serverURL, objectID); resolved != "" {
    objectID = resolved
}
// Then continue with objectID.
```

Implement `resolveAliasViaAPI` as a small HTTP call to `GET /api/v1/aliases/{alias}`.

Note: if the server is running locally and `ctxt open` talks to it via REST, this is straightforward. If `ctxt` uses the service directly (in-process), the `Service.GetObject` already resolves aliases after T4.

**Step 12.3 — test**

```bash
go build ./cmd/ctxt && echo "manual test: ctxt alias set test-doc <real-id> && ctxt open test-doc"
```

**Step 12.4 — commit**

```
git add cmd/
git commit -m "feat(cmd): resolve aliases transparently in ctxt open"
```

---

## Task 13: Integration Test

**Files:**
- create: `plugins/aliasing/plugin_test.go`

**Step 13.1 — write integration test**

Create `plugins/aliasing/plugin_test.go`:

```go
package aliasing_test

import (
    "context"
    "testing"
    "time"

    aliasing "github.com/ideacrafterslabs/ctxt-plugin-aliasing"
    "github.com/ideacrafterslabs/ctxt/internal/storage"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

// TestPluginE2E verifies the full set/resolve/list/remove cycle.
func TestPluginE2E(t *testing.T) {
    store := &mockAliasStore{}
    p := aliasing.New()
    require.NoError(t, p.Init(context.Background(), nil, pluginDepsWithStore(store)))

    ctx := context.Background()
    objectID := "obj_real_001"

    // Set alias.
    require.NoError(t, p.SetAlias(ctx, "project-charter", objectID, "global", ""))

    // Resolve.
    id, err := p.ResolveID(ctx, "project-charter", "")
    require.NoError(t, err)
    assert.Equal(t, objectID, id)

    // Unknown alias passes through.
    id, err = p.ResolveID(ctx, "obj_other", "")
    require.NoError(t, err)
    assert.Equal(t, "obj_other", id)

    // List.
    aliases, err := p.ListAliases(ctx, objectID)
    require.NoError(t, err)
    require.Len(t, aliases, 1)
    assert.Equal(t, "project-charter", aliases[0].Alias)

    // Remove.
    require.NoError(t, p.RemoveAlias(ctx, "project-charter", "global", ""))
    id, err = p.ResolveID(ctx, "project-charter", "")
    require.NoError(t, err)
    assert.Equal(t, "project-charter", id, "after removal, alias passes through unchanged")
}

func pluginDepsWithStore(store *mockAliasStore) plugin.Deps {
    // Build a minimal StorageDriver mock that returns the mockAliasStore.
    return plugin.Deps{Store: &mockStorageDriver{aliases: store}}
}
```

(Implement `mockStorageDriver` as a stub `storage.StorageDriver` that returns `store` from `Aliases()` and panics or returns nil for other methods.)

**Step 13.2 — run test, commit**

```bash
cd plugins/aliasing && go test ./... -v
```

Expected: all `PASS`.

```
git add plugins/aliasing/plugin_test.go
git commit -m "test(plugin/aliasing): add E2E integration tests"
```

---

## Task 14: Extraction-Readiness Verification

**Step 14.1 — verify standalone build**

```bash
cd ./plugins/aliasing
go build ./...
```

Expected: no errors.

**Step 14.2 — verify tests pass**

```bash
go test ./... -count=1
```

**Step 14.3 — check no relative imports**

```bash
grep -r "\"\.\./" ./plugins/aliasing/
```

Expected: zero output.

**Step 14.4 — document extraction procedure**

To extract to standalone repo:

1. Copy `plugins/aliasing/` to new repo root.
2. Remove `replace github.com/ideacrafterslabs/ctxt => ../..` from `go.mod`; pin to a tagged version.
3. Remove entry from parent `go.work`.
4. Publish as `github.com/ideacrafterslabs/ctxt-plugin-aliasing`.

**Step 14.5 — commit**

```
git add .
git commit -m "chore(plugin/aliasing): verify extraction-readiness"
```

---

## Example config.yaml

```yaml
plugins:
  - plugin: aliasing
    type: storage-hook
    config:
      enabled: true
      default_scope: global   # or "profile"
```

---

## TDD Cycle Summary

```
T1: migration test          → PASS → commit
T2: AliasStore tests        → PASS → commit
T3: go build ./...          → PASS → commit
T4: service resolver test   → PASS → commit
T5: go build module         → PASS → commit
T6: config tests            → PASS → commit
T7: store helper tests      → PASS → commit
T8: resolver tests          → PASS → commit
T9: go build plugin         → PASS → commit
T10: handler tests          → PASS → commit
T11: go build CLI           → PASS → commit
T12: manual open test       → PASS → commit
T13: E2E tests              → all PASS → commit
T14: standalone build+test  → PASS → commit
```

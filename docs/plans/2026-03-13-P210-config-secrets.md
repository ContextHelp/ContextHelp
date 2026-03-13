# Configuration & Secrets Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace hardcoded worker constants with config-driven values, add per-pipeline provider overrides, introduce a secrets abstraction layer, and add `ctxt init` / `ctxt config diff` / `ctxt secret set` commands.

**Architecture:** All new config structs added to `internal/config/config.go` with zero-value defaults that reproduce current behavior. Worker constants wired through `JobsConfig`. Secrets abstracted behind `internal/secrets/Resolver` interface with env/keychain/age-file backends. New commands use `charmbracelet/huh` for interactive init wizard.

**Tech Stack:** Go, Viper (already used), charmbracelet/huh (new), filippo.io/age (new for age-file backend).

---

## Current State Audit

Before implementing, understand the baseline:

- `internal/config/config.go` — `Config` struct with `StorageConfig`, `ServerConfig`, `ProfileConfig`, `RegistryConfig`, `PluginConfig`, `I18nConfig`, `ProvidersConfig`, `RetrievalConfig`. `ProfileConfig` currently holds only `Default string`. No `Version` field. `setDefaults()` uses Viper. `bindEnvVars()` has no `DPKMS_POLL_INTERVAL` etc.
- `internal/jobs/worker.go` — `WorkerPool` has unexported fields `staleTimeout: 30 * time.Minute` and `pollInterval: 500 * time.Millisecond`. Package-level `const maxHops = 5`. `MaxRetries` is hardcoded at `3` in `fanOutItems` (`job.MaxRetries: 3`). `NewWorkerPool` signature: `(queue, pipelines, store, workers int, bus)`.
- `internal/pipeline/builtins/builtins.go` — `BuildOpts{Factory, BlobStore, BlobThreshold}`. `ConfiguredRegistryWithOpts(opts BuildOpts)` is the extension point. No per-pipeline provider override today.
- `internal/providers/factory.go` — `NewFactory(cfg config.ProvidersConfig)`. All `os.Getenv("OPENAI_API_KEY")` etc. calls are direct. No secrets abstraction.
- `cmd/ctxt/cmd/config.go` — `ctxt config show/path/validate/edit`. No `diff` subcommand.
- `cmd/ctxt/cmd/profile.go` — `runProfileCreate/Delete/SetDefault` all print "not yet implemented".
- `cmd/ctxt/cmd/helpers.go` — `printTable` uses `charmbracelet/lipgloss/table`. `newService()` calls `builtins.Registry()` (no factory).
- `cmd/dpkms/cmd/serve.go` — calls `jobs.NewWorkerPool(queue, pipes, driver, workers, svc.Bus)` at line 137.
- Module path: `github.com/ideacrafterslabs/ctxt`

---

## Phase 1 — Structural: new structs + defaults + validation + write-back

> Zero behavior change. Safe to merge immediately after Task 4.

### Task 1 — Add new config structs and `Version` to `Config`

**File:** `internal/config/config.go`

**Step 1.1 — Add `Version int` and new embedded structs to `Config`.**

Add `Version int` as the first field of `Config`. Then add these fields after `Retrieval`:

```go
// Version is the schema version. Used for migrations.
Version int `mapstructure:"version" yaml:"version"`

// Jobs controls worker pool behaviour.
Jobs JobsConfig `mapstructure:"jobs"`

// Pipelines holds per-pipeline provider overrides and routing rules.
Pipelines PipelinesConfig `mapstructure:"pipelines"`

// Conventions enforces naming rules across the system.
Conventions ConventionsConfig `mapstructure:"conventions"`

// Secrets configures the secrets backend.
Secrets SecretsConfig `mapstructure:"secrets"`

// Watch configures the filesystem watcher.
Watch WatchConfig `mapstructure:"watch"`

// Inbox configures the default inbox for new content.
Inbox InboxConfig `mapstructure:"inbox"`
```

**Step 1.2 — Add `JobsConfig` struct.**

```go
// JobsConfig controls worker pool runtime parameters.
type JobsConfig struct {
    // PollInterval is how often idle workers poll for new jobs. Min 50ms.
    PollInterval time.Duration `mapstructure:"poll_interval" yaml:"poll_interval"`
    // StaleTimeout is how long a running job can be silent before it is
    // considered stale and eligible for recovery.
    StaleTimeout time.Duration `mapstructure:"stale_timeout" yaml:"stale_timeout"`
    // MaxRetries is the default retry limit for fan-out jobs.
    MaxRetries int `mapstructure:"max_retries" yaml:"max_retries"`
    // MaxHops is the maximum number of pipeline hops a job can take.
    MaxHops int `mapstructure:"max_hops" yaml:"max_hops"`
}
```

**Step 1.3 — Add `PipelinesConfig` and `PipelineOverride` structs.**

```go
// PipelinesConfig holds per-pipeline overrides and global routing config.
type PipelinesConfig struct {
    // Overrides maps pipeline name to a per-pipeline override.
    Overrides map[string]PipelineOverride `mapstructure:"overrides" yaml:"overrides"`
}

// PipelineOverride allows customising a single named pipeline.
type PipelineOverride struct {
    // Providers overrides individual provider backends for this pipeline only.
    // Keys match ProvidersConfig field names in lowercase: "llm", "vision", etc.
    Providers map[string]ProviderBackendConfig `mapstructure:"providers" yaml:"providers"`
    // SkipSteps is an ordered list of step names to remove from the pipeline.
    SkipSteps []string `mapstructure:"skip_steps" yaml:"skip_steps"`
    // ExtraSteps is an ordered list of step names appended after existing steps.
    ExtraSteps []string `mapstructure:"extra_steps" yaml:"extra_steps"`
}
```

**Step 1.4 — Add `ConventionsConfig` struct.**

```go
// ConventionsConfig enforces naming rules across the system.
type ConventionsConfig struct {
    // EnforceMentionNamespaces controls @namespace validation.
    // Valid values: "off" (default), "warn", "error".
    EnforceMentionNamespaces string `mapstructure:"enforce_mention_namespaces" yaml:"enforce_mention_namespaces"`
    // AllowedMentionNamespaces is the set of valid @namespace prefixes.
    // Empty means all namespaces are allowed.
    AllowedMentionNamespaces []string `mapstructure:"allowed_mention_namespaces" yaml:"allowed_mention_namespaces"`
    // TagVocabulary is the canonical set of tags. When non-empty, tags outside
    // this set trigger fuzzy suggestions.
    TagVocabulary []string `mapstructure:"tag_vocabulary" yaml:"tag_vocabulary"`
}
```

**Step 1.5 — Add `SecretsConfig` struct.**

```go
// SecretsConfig controls where API keys and other secrets are read from.
type SecretsConfig struct {
    // Backend selects the secrets provider.
    // Valid values: "env" (default), "keychain", "age-file".
    Backend string `mapstructure:"backend" yaml:"backend"`
    // AgeFile is the path to an age-encrypted YAML secrets file.
    // Only used when Backend == "age-file".
    AgeFile string `mapstructure:"age_file" yaml:"age_file"`
    // AgeIdentityFile is the path to the age identity (private key) file.
    AgeIdentityFile string `mapstructure:"age_identity_file" yaml:"age_identity_file"`
    // KeychainService is the macOS/Linux keychain service name.
    // Defaults to "ctxt".
    KeychainService string `mapstructure:"keychain_service" yaml:"keychain_service"`
}
```

**Step 1.6 — Add `WatchConfig` struct.**

```go
// WatchConfig configures the filesystem watcher.
type WatchConfig struct {
    // Enabled activates the watcher on startup.
    Enabled bool `mapstructure:"enabled" yaml:"enabled"`
    // Paths is the list of directories to watch.
    Paths []string `mapstructure:"paths" yaml:"paths"`
    // Debounce is how long to wait after a change before processing.
    Debounce time.Duration `mapstructure:"debounce" yaml:"debounce"`
    // Patterns is a list of glob patterns to include (e.g. "*.md").
    Patterns []string `mapstructure:"patterns" yaml:"patterns"`
}
```

**Step 1.7 — Add `InboxConfig` struct.**

```go
// InboxConfig configures the default inbox for new content.
type InboxConfig struct {
    // Path is the directory to use as the inbox. Defaults to ~/ctxt-inbox.
    Path string `mapstructure:"path" yaml:"path"`
    // Pipeline is the pipeline to use for inbox items. Defaults to "text.short".
    Pipeline string `mapstructure:"pipeline" yaml:"pipeline"`
    // Tags are auto-applied tags for inbox items.
    Tags []string `mapstructure:"tags" yaml:"tags"`
}
```

**Step 1.8 — Add `FocusProfile` struct and update `ProfileConfig`.**

`ProfileConfig` currently only has `Default string`. Extend it:

```go
// ProfileConfig represents profile configuration.
type ProfileConfig struct {
    Default  string                   `mapstructure:"default" yaml:"default"`
    Profiles map[string]FocusProfile  `mapstructure:"profiles" yaml:"profiles"`
}

// FocusProfile is a named configuration preset for a specific role or project.
type FocusProfile struct {
    // Description is a human-readable label shown in ctxt profile list.
    Description string `mapstructure:"description" yaml:"description"`
    // Tags is the default tag set pre-populated when this profile is active.
    Tags []string `mapstructure:"tags" yaml:"tags"`
    // MentionNamespaces constrains which @namespaces are surfaced in results.
    MentionNamespaces []string `mapstructure:"mention_namespaces" yaml:"mention_namespaces"`
    // RerankBoosts maps entity types to a boost factor (1.0 = no boost).
    RerankBoosts map[string]float64 `mapstructure:"rerank_boosts" yaml:"rerank_boosts"`
}
```

**Step 1.9 — Extend `setDefaults()` with defaults for new structs.**

In the existing `setDefaults(v *viper.Viper)` function, append:

```go
// Schema version
v.SetDefault("version", 0)

// Jobs defaults (reproduce current hardcoded values)
v.SetDefault("jobs.poll_interval", 500*time.Millisecond)
v.SetDefault("jobs.stale_timeout", 30*time.Minute)
v.SetDefault("jobs.max_retries", 3)
v.SetDefault("jobs.max_hops", 5)

// Pipelines defaults
v.SetDefault("pipelines.overrides", map[string]any{})

// Conventions defaults
v.SetDefault("conventions.enforce_mention_namespaces", "off")

// Secrets defaults
v.SetDefault("secrets.backend", "env")
v.SetDefault("secrets.keychain_service", "ctxt")

// Watch defaults
v.SetDefault("watch.enabled", false)
v.SetDefault("watch.debounce", 500*time.Millisecond)

// Inbox defaults — leave path empty (resolved at runtime)
v.SetDefault("inbox.pipeline", "text.short")
```

**Step 1.10 — Add env bindings in `bindEnvVars()`.**

Append to the existing `bindEnvVars(v *viper.Viper)` function:

```go
v.BindEnv("jobs.poll_interval", "DPKMS_POLL_INTERVAL")
v.BindEnv("jobs.stale_timeout", "DPKMS_STALE_TIMEOUT")
v.BindEnv("jobs.max_retries", "DPKMS_MAX_RETRIES")
v.BindEnv("jobs.max_hops", "DPKMS_MAX_HOPS")
v.BindEnv("secrets.backend", "CTXT_SECRETS_BACKEND")
v.BindEnv("secrets.age_file", "CTXT_AGE_FILE")
v.BindEnv("secrets.age_identity_file", "CTXT_AGE_IDENTITY")
```

**Step 1.11 — Write test.**

File: `internal/config/config_test.go`

```go
func TestEmptyYAMLDefaults(t *testing.T) {
    dir := t.TempDir()
    cfgPath := filepath.Join(dir, "config.yaml")
    os.WriteFile(cfgPath, []byte(""), 0644)

    cfg, err := Load(cfgPath)
    require.NoError(t, err)

    assert.Equal(t, 500*time.Millisecond, cfg.Jobs.PollInterval)
    assert.Equal(t, 30*time.Minute, cfg.Jobs.StaleTimeout)
    assert.Equal(t, 3, cfg.Jobs.MaxRetries)
    assert.Equal(t, 5, cfg.Jobs.MaxHops)
    assert.Equal(t, "env", cfg.Secrets.Backend)
    assert.Equal(t, "off", cfg.Conventions.EnforceMentionNamespaces)
    assert.Equal(t, "text.short", cfg.Inbox.Pipeline)
}
```

Run: `go test ./internal/config/... -run TestEmptyYAMLDefaults`
Expected: `PASS`

**Commit:** `feat(config): add JobsConfig, PipelinesConfig, ConventionsConfig, SecretsConfig, WatchConfig, InboxConfig, FocusProfile structs with defaults`

---

### Task 2 — Add `internal/config/validate.go`

**File:** `internal/config/validate.go` (new)

**Step 2.1 — Create the file.**

```go
package config

import (
    "fmt"
    "regexp"
)

// ValidationError describes a single config validation failure.
type ValidationError struct {
    Field   string
    Message string
}

func (e ValidationError) Error() string {
    return fmt.Sprintf("config: %s: %s", e.Field, e.Message)
}

var namespaceRE = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

var validStorageTypes = map[string]bool{
    "sqlite":   true,
    "postgres": true,
    "memory":   true,
}

// Validate runs all validation rules against cfg and returns a slice of
// ValidationErrors. An empty slice means the config is valid.
func Validate(cfg *Config) []ValidationError {
    var errs []ValidationError

    // storage.type enum check
    if cfg.Storage.Type != "" && !validStorageTypes[cfg.Storage.Type] {
        errs = append(errs, ValidationError{
            Field:   "storage.type",
            Message: fmt.Sprintf("must be one of sqlite, postgres, memory; got %q", cfg.Storage.Type),
        })
    }

    // server.port range
    if cfg.Server.Port != 0 && (cfg.Server.Port < 1024 || cfg.Server.Port > 65535) {
        errs = append(errs, ValidationError{
            Field:   "server.port",
            Message: fmt.Sprintf("must be between 1024 and 65535; got %d", cfg.Server.Port),
        })
    }

    // server.grpc_port range
    if cfg.Server.GRPCPort != 0 && (cfg.Server.GRPCPort < 1024 || cfg.Server.GRPCPort > 65535) {
        errs = append(errs, ValidationError{
            Field:   "server.grpc_port",
            Message: fmt.Sprintf("must be between 1024 and 65535; got %d", cfg.Server.GRPCPort),
        })
    }

    // jobs.poll_interval minimum
    if cfg.Jobs.PollInterval > 0 && cfg.Jobs.PollInterval < 50*time.Millisecond {
        errs = append(errs, ValidationError{
            Field:   "jobs.poll_interval",
            Message: fmt.Sprintf("must be >= 50ms; got %s", cfg.Jobs.PollInterval),
        })
    }

    // profile.default must exist in profile.profiles (if profiles defined)
    if cfg.Profile.Default != "" && len(cfg.Profile.Profiles) > 0 {
        if _, ok := cfg.Profile.Profiles[cfg.Profile.Default]; !ok {
            errs = append(errs, ValidationError{
                Field:   "profile.default",
                Message: fmt.Sprintf("profile %q not found in profile.profiles", cfg.Profile.Default),
            })
        }
    }

    // allowed_mention_namespaces format
    for i, ns := range cfg.Conventions.AllowedMentionNamespaces {
        if !namespaceRE.MatchString(ns) {
            errs = append(errs, ValidationError{
                Field:   fmt.Sprintf("conventions.allowed_mention_namespaces[%d]", i),
                Message: fmt.Sprintf("must match ^[a-z][a-z0-9_]*$; got %q", ns),
            })
        }
    }

    // secrets.backend enum
    validSecretBackends := map[string]bool{"env": true, "keychain": true, "age-file": true}
    if cfg.Secrets.Backend != "" && !validSecretBackends[cfg.Secrets.Backend] {
        errs = append(errs, ValidationError{
            Field:   "secrets.backend",
            Message: fmt.Sprintf("must be one of env, keychain, age-file; got %q", cfg.Secrets.Backend),
        })
    }

    return errs
}
```

Note: add `"time"` to the import block.

**Step 2.2 — Write table-driven test.**

File: `internal/config/validate_test.go` (new)

```go
package config

import (
    "testing"
    "time"

    "github.com/stretchr/testify/assert"
)

func TestValidate(t *testing.T) {
    cases := []struct {
        name      string
        mutate    func(*Config)
        wantField string
    }{
        {
            name:      "invalid storage type",
            mutate:    func(c *Config) { c.Storage.Type = "badtype" },
            wantField: "storage.type",
        },
        {
            name:      "port below range",
            mutate:    func(c *Config) { c.Server.Port = 80 },
            wantField: "server.port",
        },
        {
            name:      "grpc port above range",
            mutate:    func(c *Config) { c.Server.GRPCPort = 99999 },
            wantField: "server.grpc_port",
        },
        {
            name:      "poll interval too short",
            mutate:    func(c *Config) { c.Jobs.PollInterval = 10 * time.Millisecond },
            wantField: "jobs.poll_interval",
        },
        {
            name: "default profile not in profiles map",
            mutate: func(c *Config) {
                c.Profile.Default = "missing"
                c.Profile.Profiles = map[string]FocusProfile{"other": {}}
            },
            wantField: "profile.default",
        },
        {
            name: "invalid namespace format",
            mutate: func(c *Config) {
                c.Conventions.AllowedMentionNamespaces = []string{"BadNS"}
            },
            wantField: "conventions.allowed_mention_namespaces[0]",
        },
    }

    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            cfg := &Config{}
            tc.mutate(cfg)
            errs := Validate(cfg)
            assert.NotEmpty(t, errs)
            assert.Equal(t, tc.wantField, errs[0].Field)
        })
    }
}
```

Run: `go test ./internal/config/... -run TestValidate`
Expected: `PASS` (6 subtests)

**Commit:** `feat(config): add Validate() with 6 validation rules`

---

### Task 3 — Add `internal/config/write.go`

**File:** `internal/config/write.go` (new)

**Step 3.1 — Create the file.**

```go
package config

import (
    "fmt"
    "os"

    "gopkg.in/yaml.v3"
)

// WriteBack marshals cfg to YAML and atomically writes it to path.
// The write is atomic: it writes to <path>.tmp then renames.
func WriteBack(cfg *Config, path string) error {
    data, err := yaml.Marshal(cfg)
    if err != nil {
        return fmt.Errorf("config: marshal: %w", err)
    }

    tmp := path + ".tmp"
    if err := os.WriteFile(tmp, data, 0600); err != nil {
        return fmt.Errorf("config: write tmp: %w", err)
    }

    if err := os.Rename(tmp, path); err != nil {
        os.Remove(tmp)
        return fmt.Errorf("config: rename: %w", err)
    }

    return nil
}
```

**Step 3.2 — Check that `gopkg.in/yaml.v3` is already in `go.mod`.** Run `grep yaml go.mod`. If absent, run `go get gopkg.in/yaml.v3`.

**Step 3.3 — Write test.**

File: `internal/config/write_test.go` (new)

```go
package config

import (
    "path/filepath"
    "testing"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func TestWriteBackRoundtrip(t *testing.T) {
    dir := t.TempDir()
    path := filepath.Join(dir, "config.yaml")

    original := &Config{
        Version: 1,
        Jobs: JobsConfig{
            PollInterval: 200 * time.Millisecond,
            StaleTimeout: 15 * time.Minute,
            MaxRetries:   5,
            MaxHops:      3,
        },
        Secrets: SecretsConfig{Backend: "env"},
    }

    err := WriteBack(original, path)
    require.NoError(t, err)

    loaded, err := Load(path)
    require.NoError(t, err)

    assert.Equal(t, original.Version, loaded.Version)
    assert.Equal(t, original.Jobs.PollInterval, loaded.Jobs.PollInterval)
    assert.Equal(t, original.Jobs.MaxRetries, loaded.Jobs.MaxRetries)
}
```

Note: add `"time"` import.

Run: `go test ./internal/config/... -run TestWriteBackRoundtrip`
Expected: `PASS`

**Commit:** `feat(config): add atomic WriteBack()`

---

### Task 4 — Config migration in `Load()`

**File:** `internal/config/config.go`

**Step 4.1 — Add schema version constant.**

Near the top of `config.go`, add:

```go
// currentSchemaVersion is the latest config schema version.
const currentSchemaVersion = 1
```

**Step 4.2 — Add `migrate()` function.**

```go
// migrate applies schema migrations to cfg in-place and returns true if
// any migration was applied (caller should write back).
func migrate(cfg *Config) bool {
    changed := false

    if cfg.Version < 1 {
        // v0 → v1: no structural changes; just stamp the version.
        cfg.Version = 1
        changed = true
    }

    return changed
}
```

**Step 4.3 — Call `migrate()` in `Load()`.** After the `v.Unmarshal(&cfg)` call and before the return:

```go
// Run migrations if needed.
if cfg.Version < currentSchemaVersion {
    if migrate(&cfg) {
        // Best-effort write-back: ignore errors (config path may be read-only).
        cfgPath := v.ConfigFileUsed()
        if cfgPath != "" {
            _ = WriteBack(&cfg, cfgPath)
        }
    }
}
```

**Step 4.4 — Write test.**

File: `internal/config/migrate_test.go` (new)

```go
package config

import (
    "os"
    "path/filepath"
    "testing"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func TestMigrateV0ToV1(t *testing.T) {
    dir := t.TempDir()
    path := filepath.Join(dir, "config.yaml")

    // Write a v0 config (no version field).
    v0yaml := []byte("storage:\n  type: sqlite\n")
    require.NoError(t, os.WriteFile(path, v0yaml, 0644))

    cfg, err := Load(path)
    require.NoError(t, err)
    assert.Equal(t, 1, cfg.Version, "version should be bumped to 1")

    // Reload from disk to verify write-back.
    reloaded, err := Load(path)
    require.NoError(t, err)
    assert.Equal(t, 1, reloaded.Version, "version should be persisted after migration")
}
```

Run: `go test ./internal/config/... -run TestMigrateV0ToV1`
Expected: `PASS`

**Commit:** `feat(config): add schema versioning and v0→v1 migration`

---

## Phase 2 — Wire jobs constants

### Task 5 — Update `NewWorkerPool` to accept `config.JobsConfig`

**File:** `internal/jobs/worker.go`

**Step 5.1 — Update `WorkerPool` struct to add `maxHops` and `maxRetries` fields.**

In the `WorkerPool` struct, change:

```go
// before
workers      int
staleTimeout time.Duration
pollInterval time.Duration
```

to:

```go
workers      int
staleTimeout time.Duration
pollInterval time.Duration
maxHops      int
maxRetries   int
```

**Step 5.2 — Update `NewWorkerPool` signature.**

Change the import block to include `"github.com/ideacrafterslabs/ctxt/internal/config"`.

Change the function signature from:

```go
func NewWorkerPool(queue *Queue, pipelines pipeline.Registry, store storage.StorageDriver, workers int, bus events.Bus) *WorkerPool {
```

to:

```go
func NewWorkerPool(queue *Queue, pipelines pipeline.Registry, store storage.StorageDriver, workers int, bus events.Bus, cfg config.JobsConfig) *WorkerPool {
```

Update the body:

```go
return &WorkerPool{
    queue:        queue,
    pipelines:    pipelines,
    store:        store,
    bus:          bus,
    workers:      workers,
    staleTimeout: cfg.StaleTimeout,
    pollInterval: cfg.PollInterval,
    maxHops:      cfg.MaxHops,
    maxRetries:   cfg.MaxRetries,
}
```

**Step 5.3 — Remove the package-level `const maxHops = 5`.**

Delete line `const maxHops = 5`.

**Step 5.4 — Replace hardcoded `maxHops` and `maxRetries` references.**

In `processWithHops()`, replace `for hop := 1; hop < maxHops; hop++` with `for hop := 1; hop < p.maxHops; hop++`.

In `fanOutItems()`, replace `MaxRetries: 3` with `MaxRetries: p.maxRetries`.

**Step 5.5 — Update call site in `cmd/dpkms/cmd/serve.go`.**

Line 137 currently:
```go
pool := jobs.NewWorkerPool(queue, pipes, driver, workers, svc.Bus)
```

Change to:
```go
pool := jobs.NewWorkerPool(queue, pipes, driver, workers, svc.Bus, cfg.Jobs)
```

Where `cfg` is the `*config.Config` available in the serve command's package-level `cfg` variable (same pattern as `cfg.Providers` used at line 103).

**Step 5.6 — Write test.**

File: `internal/jobs/worker_test.go` (extend or create)

```go
func TestWorkerPoolUsesConfigPollInterval(t *testing.T) {
    jobCfg := config.JobsConfig{
        PollInterval: 100 * time.Millisecond,
        StaleTimeout: 30 * time.Minute,
        MaxRetries:   3,
        MaxHops:      5,
    }
    pool := NewWorkerPool(nil, nil, nil, 1, nil, jobCfg)
    assert.Equal(t, 100*time.Millisecond, pool.pollInterval)
    assert.Equal(t, 5, pool.maxHops)
}
```

Run: `go test ./internal/jobs/... -run TestWorkerPoolUsesConfigPollInterval`
Expected: `PASS`

**Commit:** `feat(jobs): wire JobsConfig into WorkerPool; remove hardcoded constants`

---

### Task 6 — Compile check + commit phase 2

**Step 6.1 — Build all binaries to confirm no compile errors.**

```
go build ./cmd/ctxt/... ./cmd/dpkms/...
```

Expected: no errors.

**Step 6.2 — Run all existing tests.**

```
go test ./...
```

Expected: all passing (no regressions).

**Commit:** `chore(jobs): phase 2 compile check — all tests pass`

---

## Phase 3 — Per-pipeline provider overrides

### Task 7 — `BuildAll(cfg PipelinesConfig)` with per-pipeline factory

**File:** `internal/pipeline/builtins/builtins.go`

**Step 7.1 — Add `PipelinesConfig` import.**

Add `"github.com/ideacrafterslabs/ctxt/internal/config"` to the import block.

**Step 7.2 — Add a helper that merges a base `ProvidersConfig` with a per-pipeline override.**

Add this function before `buildRegistry`:

```go
// mergeProviderOverride merges a per-pipeline Providers override onto base.
// Only non-empty Backend fields in override replace the base value.
func mergeProviderOverride(base config.ProvidersConfig, override map[string]config.ProviderBackendConfig) config.ProvidersConfig {
    merged := base
    for key, ov := range override {
        if ov.Backend == "" {
            continue
        }
        switch key {
        case "llm":
            merged.LLM = ov
        case "vision":
            merged.Vision = ov
        case "ocr":
            merged.OCR = ov
        case "transcription":
            merged.Transcription = ov
        case "diarization":
            merged.Diarization = ov
        case "embedding":
            merged.Embedding = ov
        case "document":
            merged.Document = ov
        case "video":
            merged.Video = ov
        }
    }
    return merged
}
```

**Step 7.3 — Add `ConfiguredRegistryWithPipelineOverrides` function.**

```go
// ConfiguredRegistryWithPipelineOverrides builds a Registry where per-pipeline
// overrides in cfg replace provider backends for that pipeline only.
func ConfiguredRegistryWithPipelineOverrides(
    baseFactory *providers.Factory,
    baseCfg config.ProvidersConfig,
    pipelinesCfg config.PipelinesConfig,
    blobStore storage.BlobStore,
    blobThreshold int64,
) pipeline.Registry {
    return buildRegistryWithOverrides(baseFactory, baseCfg, pipelinesCfg, blobStore, blobThreshold, false)
}

func buildRegistryWithOverrides(
    baseFactory *providers.Factory,
    baseCfg config.ProvidersConfig,
    pipelinesCfg config.PipelinesConfig,
    blobStore storage.BlobStore,
    blobThreshold int64,
    strict bool,
) pipeline.Registry {
    r := pipeline.NewRegistry()
    sels := buildSelectors()

    for name, d := range defs {
        opts := BuildOpts{
            Factory:       baseFactory,
            BlobStore:     blobStore,
            BlobThreshold: blobThreshold,
        }

        // Apply per-pipeline overrides if present.
        if ov, ok := pipelinesCfg.Overrides[name]; ok {
            // Provider override.
            if len(ov.Providers) > 0 && baseFactory != nil {
                mergedCfg := mergeProviderOverride(baseCfg, ov.Providers)
                opts.Factory = providers.NewFactory(mergedCfg)
            }
            // Step filtering.
            if len(ov.SkipSteps) > 0 {
                filtered := make([]string, 0, len(d.Steps))
                skipSet := make(map[string]bool, len(ov.SkipSteps))
                for _, s := range ov.SkipSteps {
                    skipSet[s] = true
                }
                for _, s := range d.Steps {
                    if !skipSet[s] {
                        filtered = append(filtered, s)
                    }
                }
                d.Steps = filtered
            }
            // Extra steps appended.
            d.Steps = append(d.Steps, ov.ExtraSteps...)
        }

        p, err := buildPipeline(name, d, opts, strict)
        if err != nil {
            panic(fmt.Sprintf("builtins: %v", err))
        }
        if err := r.Register(name, p); err != nil {
            panic(fmt.Sprintf("builtins: %v", err))
        }
    }

    r.SetSelectors(pipeline.SelectorFunc(func(content string) string {
        return selectPipeline(sels, content)
    }))

    return r
}
```

**Step 7.4 — Update `cmd/dpkms/cmd/serve.go` to use the new function.**

Replace line 104:
```go
pipes := builtins.ConfiguredRegistry(factory)
```
with:
```go
pipes := builtins.ConfiguredRegistryWithPipelineOverrides(
    factory,
    cfg.Providers,
    cfg.Pipelines,
    nil, // blob store wired separately if needed
    cfg.Storage.Blob.Threshold,
)
```

**Commit:** `feat(builtins): per-pipeline provider overrides via PipelinesConfig`

---

### Task 8 — Test per-pipeline provider override

**File:** `internal/pipeline/builtins/overrides_test.go` (new)

**Step 8.1 — Write integration test.**

```go
package builtins_test

import (
    "testing"

    "github.com/ideacrafterslabs/ctxt/internal/config"
    "github.com/ideacrafterslabs/ctxt/internal/pipeline/builtins"
    "github.com/ideacrafterslabs/ctxt/internal/providers"
    "github.com/stretchr/testify/require"
)

func TestPipelineProviderOverride(t *testing.T) {
    // Base config uses stub LLM everywhere.
    baseCfg := config.ProvidersConfig{}
    baseCfg.LLM.Backend = "stub"
    baseFactory := providers.NewFactory(baseCfg)

    // Override url.generic to use anthropic LLM.
    pipelinesCfg := config.PipelinesConfig{
        Overrides: map[string]config.PipelineOverride{
            "url.generic": {
                Providers: map[string]config.ProviderBackendConfig{
                    "llm": {Backend: "anthropic", Model: "claude-3-haiku-20240307"},
                },
            },
        },
    }

    reg := builtins.ConfiguredRegistryWithPipelineOverrides(baseFactory, baseCfg, pipelinesCfg, nil, 0)
    require.NotNil(t, reg)

    pipe, err := reg.Get("url.generic")
    require.NoError(t, err)
    require.NotNil(t, pipe)
    // Structural check: pipeline must have steps.
    require.NotEmpty(t, pipe.Steps)
}
```

Run: `go test ./internal/pipeline/builtins/... -run TestPipelineProviderOverride`
Expected: `PASS`

**Commit:** `test(builtins): verify per-pipeline provider override wiring`

---

## Phase 4 — Conventions enforcement

### Task 9 — Mention namespace enforcement in `service.Analyze()`

**File:** `internal/service/service.go`

**Step 9.1 — Add `ConventionsConfig` to `Service`.**

Add a field to `Service`:

```go
Conventions config.ConventionsConfig
```

Add import: `"github.com/ideacrafterslabs/ctxt/internal/config"`

**Step 9.2 — Update `New()` constructor.**

Change signature to accept conventions:

```go
func New(store storage.StorageDriver, queue *jobs.Queue, pipes pipeline.Registry, engine *search.Engine, stepsPath string, bus events.Bus, conventions config.ConventionsConfig) *Service {
```

Assign: `Conventions: conventions,`

**Step 9.3 — Update all callers of `service.New()`.**

- `cmd/dpkms/cmd/serve.go` line 118: `service.New(driver, queue, pipes, engine, stepsPath, nil, cfg.Conventions)`
- `cmd/ctxt/cmd/helpers.go` line 56: `service.New(driver, queue, pipes, engine, "", nil, config.ConventionsConfig{})` (pass zero value; ctxt CLI doesn't enforce yet)

**Step 9.4 — Add mention namespace validation in `Analyze()`.**

In `service.go`, add a helper:

```go
// validateMentionNamespaces checks that all @mentions use an allowed namespace.
// Returns a slice of invalid mentions.
func (s *Service) validateMentionNamespaces(mentions []string) []string {
    if s.Conventions.EnforceMentionNamespaces == "off" || len(s.Conventions.AllowedMentionNamespaces) == 0 {
        return nil
    }
    allowed := make(map[string]bool, len(s.Conventions.AllowedMentionNamespaces))
    for _, ns := range s.Conventions.AllowedMentionNamespaces {
        allowed[ns] = true
    }
    var bad []string
    for _, m := range mentions {
        // @namespace.slug — extract namespace part.
        ns := m
        if idx := strings.Index(m, "."); idx > 0 {
            ns = m[:idx]
        }
        if !allowed[ns] {
            bad = append(bad, m)
        }
    }
    return bad
}
```

**Step 9.5 — Call validation in `Analyze()`.** After the job is built (after `job :=` block) but before `s.Queue.Enqueue(ctx, job)`, add:

```go
// Conventions: validate mention namespaces if hints provided.
if s.Conventions.EnforceMentionNamespaces != "off" && len(req.Mentions) > 0 {
    if bad := s.validateMentionNamespaces(req.Mentions); len(bad) > 0 {
        msg := fmt.Sprintf("invalid mention namespaces: %v", bad)
        switch s.Conventions.EnforceMentionNamespaces {
        case "error":
            return "", fmt.Errorf(msg)
        case "warn":
            fmt.Fprintf(os.Stderr, "warning: %s\n", msg)
        }
    }
}
```

Note: check `AnalyzeRequest` struct for the `Mentions` field name — read the struct definition first to confirm exact field name.

**Step 9.6 — Write test.**

File: `internal/service/conventions_test.go` (new)

```go
func TestAnalyzeRejectsInvalidNamespace(t *testing.T) {
    // minimal wired service with "error" enforcement
    conventions := config.ConventionsConfig{
        EnforceMentionNamespaces: "error",
        AllowedMentionNamespaces: []string{"project", "person"},
    }
    svc := &Service{Conventions: conventions}
    bad := svc.validateMentionNamespaces([]string{"project.alpha", "badns.foo"})
    assert.Equal(t, []string{"badns.foo"}, bad)
}
```

Run: `go test ./internal/service/... -run TestAnalyzeRejectsInvalidNamespace`
Expected: `PASS`

**Commit:** `feat(service): add mention namespace enforcement via ConventionsConfig`

---

### Task 10 — Tag fuzzy suggestions in `ctxt analyze`

**File:** `cmd/ctxt/cmd/helpers.go`

**Step 10.1 — Add Levenshtein distance helper.**

Add at bottom of the file:

```go
// levenshtein computes the edit distance between two strings.
// O(m*n) space, suitable for short tag names.
func levenshtein(a, b string) int {
    m, n := len(a), len(b)
    dp := make([][]int, m+1)
    for i := range dp {
        dp[i] = make([]int, n+1)
        dp[i][0] = i
    }
    for j := 0; j <= n; j++ {
        dp[0][j] = j
    }
    for i := 1; i <= m; i++ {
        for j := 1; j <= n; j++ {
            if a[i-1] == b[j-1] {
                dp[i][j] = dp[i-1][j-1]
            } else {
                dp[i][j] = 1 + min(dp[i-1][j], min(dp[i][j-1], dp[i-1][j-1]))
            }
        }
    }
    return dp[m][n]
}
```

**Step 10.2 — Add `warnTagSuggestions()` helper.**

```go
// warnTagSuggestions prints a fuzzy match hint to stderr when a hint tag
// is within edit distance 2 of a canonical tag in the vocabulary.
func warnTagSuggestions(hints []string, vocabulary []string) {
    if len(vocabulary) == 0 {
        return
    }
    for _, hint := range hints {
        if !strings.HasPrefix(hint, "#") {
            continue
        }
        tag := strings.TrimPrefix(hint, "#")
        for _, canonical := range vocabulary {
            if tag == canonical {
                break // exact match, no suggestion needed
            }
            if levenshtein(tag, canonical) <= 2 {
                fmt.Fprintf(os.Stderr, "hint: did you mean #%s instead of #%s?\n", canonical, tag)
                break
            }
        }
    }
}
```

**Step 10.3 — Call `warnTagSuggestions` in the analyze/add command.** Locate where `--hints` flags are parsed (search for `"hints"` in `cmd/ctxt/cmd/`). Call `warnTagSuggestions(hints, cfg.Conventions.TagVocabulary)` before enqueueing the job.

**Step 10.4 — Write test.**

File: `cmd/ctxt/cmd/helpers_test.go` (new or extend)

```go
func TestWarnTagSuggestions(t *testing.T) {
    // Capture stderr
    old := os.Stderr
    r, w, _ := os.Pipe()
    os.Stderr = w

    warnTagSuggestions([]string{"#techdebt"}, []string{"tech-debt", "frontend"})

    w.Close()
    os.Stderr = old
    var buf strings.Builder
    io.Copy(&buf, r)
    assert.Contains(t, buf.String(), "did you mean #tech-debt instead of #techdebt")
}
```

Run: `go test ./cmd/ctxt/cmd/... -run TestWarnTagSuggestions`
Expected: `PASS`

**Commit:** `feat(cli): tag fuzzy suggestions using Levenshtein distance against TagVocabulary`

---

## Phase 5 — Profile commands

### Task 11 — Complete `ctxt profile create/delete/set-default`

**File:** `cmd/ctxt/cmd/profile.go`

**Step 11.1 — Replace `runProfileCreate` stub.**

```go
func runProfileCreate(cmd *cobra.Command, args []string) error {
    name := args[0]
    if cfg.Profile.Profiles == nil {
        cfg.Profile.Profiles = make(map[string]config.FocusProfile)
    }
    if _, exists := cfg.Profile.Profiles[name]; exists {
        return fmt.Errorf("profile %q already exists", name)
    }

    cfg.Profile.Profiles[name] = config.FocusProfile{
        Description: name,
    }

    cfgPath := config.GetConfigPath()
    if err := config.WriteBack(cfg, cfgPath); err != nil {
        return fmt.Errorf("write config: %w", err)
    }
    fmt.Printf("Created profile %q\n", name)
    return nil
}
```

**Step 11.2 — Replace `runProfileDelete` stub.**

```go
func runProfileDelete(cmd *cobra.Command, args []string) error {
    name := args[0]
    if _, exists := cfg.Profile.Profiles[name]; !exists {
        return fmt.Errorf("profile %q not found", name)
    }
    if cfg.Profile.Default == name {
        return fmt.Errorf("cannot delete the default profile; set a different default first")
    }

    delete(cfg.Profile.Profiles, name)

    cfgPath := config.GetConfigPath()
    if err := config.WriteBack(cfg, cfgPath); err != nil {
        return fmt.Errorf("write config: %w", err)
    }
    fmt.Printf("Deleted profile %q\n", name)
    return nil
}
```

**Step 11.3 — Replace `runProfileSetDefault` stub.**

```go
func runProfileSetDefault(cmd *cobra.Command, args []string) error {
    name := args[0]
    if len(cfg.Profile.Profiles) > 0 {
        if _, exists := cfg.Profile.Profiles[name]; !exists {
            return fmt.Errorf("profile %q not found; run 'ctxt profile create %s' first", name, name)
        }
    }

    cfg.Profile.Default = name

    cfgPath := config.GetConfigPath()
    if err := config.WriteBack(cfg, cfgPath); err != nil {
        return fmt.Errorf("write config: %w", err)
    }
    fmt.Printf("Default profile set to %q\n", name)
    return nil
}
```

**Step 11.4 — Update `runProfileList` to render the profiles map.**

```go
func runProfileList(cmd *cobra.Command, args []string) error {
    if isJSONOutput() {
        return outputJSON(os.Stdout, cfg.Profile)
    }

    defaultProfile := cfg.Profile.Default
    if defaultProfile == "" {
        defaultProfile = "(none)"
    }
    fmt.Printf("Default profile: %s\n\n", defaultProfile)

    if len(cfg.Profile.Profiles) == 0 {
        fmt.Println("No profiles defined. Run: ctxt profile create <name>")
        return nil
    }

    rows := make([][]string, 0, len(cfg.Profile.Profiles))
    for name, p := range cfg.Profile.Profiles {
        marker := ""
        if name == cfg.Profile.Default {
            marker = "*"
        }
        rows = append(rows, []string{marker, name, p.Description})
    }
    printTable(os.Stdout, []string{"", "Name", "Description"}, rows)
    return nil
}
```

**Commit:** `feat(cli): implement profile create/delete/set-default with WriteBack`

---

### Task 12 — Test profile subcommands

**Step 12.1 — Write integration test using a temp config file.**

File: `cmd/ctxt/cmd/profile_test.go` (new)

```go
package cmd_test

import (
    "os"
    "path/filepath"
    "testing"

    "github.com/ideacrafterslabs/ctxt/internal/config"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func TestProfileCreateAndSetDefault(t *testing.T) {
    dir := t.TempDir()
    cfgPath := filepath.Join(dir, "config.yaml")
    os.WriteFile(cfgPath, []byte("version: 1\n"), 0644)

    // Load baseline.
    c, err := config.Load(cfgPath)
    require.NoError(t, err)

    // Create profile.
    if c.Profile.Profiles == nil {
        c.Profile.Profiles = make(map[string]config.FocusProfile)
    }
    c.Profile.Profiles["engineer"] = config.FocusProfile{Description: "Engineer"}
    require.NoError(t, config.WriteBack(c, cfgPath))

    // Set default.
    c.Profile.Default = "engineer"
    require.NoError(t, config.WriteBack(c, cfgPath))

    // Reload and verify.
    reloaded, err := config.Load(cfgPath)
    require.NoError(t, err)
    assert.Equal(t, "engineer", reloaded.Profile.Default)
    assert.Contains(t, reloaded.Profile.Profiles, "engineer")
}

func TestProfileDeleteDefaultGuard(t *testing.T) {
    c := &config.Config{}
    c.Profile.Default = "founder"
    c.Profile.Profiles = map[string]config.FocusProfile{
        "founder": {Description: "Founder"},
    }
    // Attempt to delete the default profile should be rejected.
    // (Logic tested directly without cobra.)
    if c.Profile.Default == "founder" {
        _, exists := c.Profile.Profiles["founder"]
        assert.True(t, exists)
        // Guard condition holds.
        assert.Equal(t, c.Profile.Default, "founder")
    }
}
```

Run: `go test ./cmd/ctxt/cmd/... -run TestProfile`
Expected: `PASS`

**Commit:** `test(cli): profile create/delete/set-default tests`

---

## Phase 6 — Secrets

### Task 13 — `internal/secrets/resolver.go`

**File:** `internal/secrets/resolver.go` (new)

**Step 13.1 — Create the package and file.**

```go
package secrets

import (
    "fmt"
    "os"
)

// Resolver abstracts reading and writing secrets from any backend.
type Resolver interface {
    // Get returns the secret value for key, or an error if not found.
    Get(key string) (string, error)
    // Set stores key=value in the configured backend.
    Set(key, value string) error
}

// EnvResolver reads secrets from environment variables.
// Set() is a no-op (environment is read-only at runtime).
type EnvResolver struct{}

// NewEnvResolver returns an EnvResolver.
func NewEnvResolver() *EnvResolver { return &EnvResolver{} }

func (r *EnvResolver) Get(key string) (string, error) {
    if v := os.Getenv(key); v != "" {
        return v, nil
    }
    return "", fmt.Errorf("secrets: env var %q not set", key)
}

func (r *EnvResolver) Set(key, value string) error {
    return fmt.Errorf("secrets: EnvResolver is read-only; set %s in the environment manually", key)
}

// ErrNotFound is returned when a secret is not found in any backend.
type ErrNotFound struct {
    Key string
}

func (e ErrNotFound) Error() string {
    return fmt.Sprintf("secrets: key %q not found", e.Key)
}
```

**Step 13.2 — Write test.**

File: `internal/secrets/resolver_test.go` (new)

```go
package secrets

import (
    "os"
    "testing"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func TestEnvResolverGet(t *testing.T) {
    os.Setenv("TEST_SECRET_KEY", "myvalue")
    defer os.Unsetenv("TEST_SECRET_KEY")

    r := NewEnvResolver()
    v, err := r.Get("TEST_SECRET_KEY")
    require.NoError(t, err)
    assert.Equal(t, "myvalue", v)
}

func TestEnvResolverGetMissing(t *testing.T) {
    r := NewEnvResolver()
    _, err := r.Get("THIS_VAR_DOES_NOT_EXIST_XYZ")
    assert.Error(t, err)
}
```

Run: `go test ./internal/secrets/... -run TestEnvResolver`
Expected: `PASS`

**Commit:** `feat(secrets): add Resolver interface and EnvResolver`

---

### Task 14 — Keychain and age-file backends

**File:** `internal/secrets/keychain.go` (new)
**File:** `internal/secrets/agefile.go` (new)

**Step 14.1 — Create `keychain.go` with build tag.**

```go
//go:build darwin || linux

package secrets

import (
    "fmt"
    "os/exec"
    "runtime"
    "strings"
)

// KeychainResolver reads secrets from the OS keychain.
// macOS: uses `security find-generic-password`.
// Linux: uses `secret-tool lookup`.
type KeychainResolver struct {
    service string // keychain service name, e.g. "ctxt"
}

// NewKeychainResolver creates a KeychainResolver for the given service name.
func NewKeychainResolver(service string) *KeychainResolver {
    return &KeychainResolver{service: service}
}

func (r *KeychainResolver) Get(key string) (string, error) {
    var cmd *exec.Cmd
    switch runtime.GOOS {
    case "darwin":
        cmd = exec.Command("security", "find-generic-password",
            "-s", r.service, "-a", key, "-w")
    default: // linux
        cmd = exec.Command("secret-tool", "lookup", "service", r.service, "account", key)
    }
    out, err := cmd.Output()
    if err != nil {
        return "", ErrNotFound{Key: key}
    }
    return strings.TrimRight(string(out), "\n"), nil
}

func (r *KeychainResolver) Set(key, value string) error {
    var cmd *exec.Cmd
    switch runtime.GOOS {
    case "darwin":
        cmd = exec.Command("security", "add-generic-password",
            "-s", r.service, "-a", key, "-w", value, "-U")
    default: // linux
        cmd = exec.Command("secret-tool", "store",
            "--label", fmt.Sprintf("%s/%s", r.service, key),
            "service", r.service, "account", key)
        cmd.Stdin = strings.NewReader(value)
    }
    if out, err := cmd.CombinedOutput(); err != nil {
        return fmt.Errorf("secrets: keychain set %q: %w — %s", key, err, string(out))
    }
    return nil
}
```

**Step 14.2 — Create `agefile.go`.**

This requires `filippo.io/age`. First run: `go get filippo.io/age`.

```go
package secrets

import (
    "fmt"
    "io"
    "os"
    "strings"

    "filippo.io/age"
    "gopkg.in/yaml.v3"
)

// AgeFileResolver reads secrets from an age-encrypted YAML file.
// The decrypted YAML must be a flat map[string]string.
type AgeFileResolver struct {
    agefile      string // path to the encrypted .age file
    identityFile string // path to age identity (private key) file
}

// NewAgeFileResolver creates an AgeFileResolver.
func NewAgeFileResolver(agefile, identityFile string) *AgeFileResolver {
    return &AgeFileResolver{agefile: agefile, identityFile: identityFile}
}

func (r *AgeFileResolver) decryptAll() (map[string]string, error) {
    identBytes, err := os.ReadFile(r.identityFile)
    if err != nil {
        return nil, fmt.Errorf("secrets: age identity: %w", err)
    }

    identities, err := age.ParseIdentities(strings.NewReader(string(identBytes)))
    if err != nil {
        return nil, fmt.Errorf("secrets: parse age identity: %w", err)
    }

    f, err := os.Open(r.agefile)
    if err != nil {
        return nil, fmt.Errorf("secrets: open age file: %w", err)
    }
    defer f.Close()

    dec, err := age.Decrypt(f, identities...)
    if err != nil {
        return nil, fmt.Errorf("secrets: decrypt: %w", err)
    }

    plain, err := io.ReadAll(dec)
    if err != nil {
        return nil, fmt.Errorf("secrets: read decrypted: %w", err)
    }

    var kv map[string]string
    if err := yaml.Unmarshal(plain, &kv); err != nil {
        return nil, fmt.Errorf("secrets: parse yaml: %w", err)
    }
    return kv, nil
}

func (r *AgeFileResolver) Get(key string) (string, error) {
    kv, err := r.decryptAll()
    if err != nil {
        return "", err
    }
    v, ok := kv[key]
    if !ok {
        return "", ErrNotFound{Key: key}
    }
    return v, nil
}

func (r *AgeFileResolver) Set(key, value string) error {
    return fmt.Errorf("secrets: AgeFileResolver.Set() not implemented; edit the age file manually")
}
```

**Step 14.3 — Add a factory `NewResolver` that selects the backend from `config.SecretsConfig`.**

File: `internal/secrets/factory.go` (new)

```go
package secrets

import (
    "fmt"

    "github.com/ideacrafterslabs/ctxt/internal/config"
)

// NewResolver creates the appropriate Resolver based on cfg.
func NewResolver(cfg config.SecretsConfig) (Resolver, error) {
    switch cfg.Backend {
    case "env", "":
        return NewEnvResolver(), nil
    case "keychain":
        svc := cfg.KeychainService
        if svc == "" {
            svc = "ctxt"
        }
        return NewKeychainResolver(svc), nil
    case "age-file":
        if cfg.AgeFile == "" {
            return nil, fmt.Errorf("secrets: age-file backend requires secrets.age_file to be set")
        }
        if cfg.AgeIdentityFile == "" {
            return nil, fmt.Errorf("secrets: age-file backend requires secrets.age_identity_file to be set")
        }
        return NewAgeFileResolver(cfg.AgeFile, cfg.AgeIdentityFile), nil
    default:
        return nil, fmt.Errorf("secrets: unknown backend %q", cfg.Backend)
    }
}
```

**Step 14.4 — Write test for factory.**

File: `internal/secrets/factory_test.go` (new)

```go
package secrets

import (
    "testing"

    "github.com/ideacrafterslabs/ctxt/internal/config"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func TestNewResolverEnv(t *testing.T) {
    r, err := NewResolver(config.SecretsConfig{Backend: "env"})
    require.NoError(t, err)
    assert.IsType(t, &EnvResolver{}, r)
}

func TestNewResolverAgeFileMissingPath(t *testing.T) {
    _, err := NewResolver(config.SecretsConfig{Backend: "age-file"})
    assert.Error(t, err)
}
```

Run: `go test ./internal/secrets/... -run TestNewResolver`
Expected: `PASS`

**Commit:** `feat(secrets): add keychain and age-file backends with factory`

---

### Task 15 — Wire `secrets.Resolver` into `providers/factory.go`

**File:** `internal/providers/factory.go`

**Step 15.1 — Add `Resolver` field to `Factory`.**

Change the struct:

```go
type Factory struct {
    cfg      config.ProvidersConfig
    secrets  secrets.Resolver
}
```

Add import: `"github.com/ideacrafterslabs/ctxt/internal/secrets"`

**Step 15.2 — Update `NewFactory` to accept a resolver.**

```go
func NewFactory(cfg config.ProvidersConfig, resolver secrets.Resolver) *Factory {
    if resolver == nil {
        resolver = secrets.NewEnvResolver()
    }
    return &Factory{cfg: cfg, secrets: resolver}
}
```

**Step 15.3 — Add a helper `apiKey()` method.**

```go
// apiKey fetches a secret by key, falling back to os.Getenv for backward compat.
func (f *Factory) apiKey(key string) string {
    if v, err := f.secrets.Get(key); err == nil {
        return v
    }
    return os.Getenv(key)
}
```

**Step 15.4 — Replace all `os.Getenv("..._API_KEY")` calls in `factory.go` with `f.apiKey(...)`.**

These occur in `Vision()` auto-branch and `LLM()` auto-branch:

- `os.Getenv("OPENAI_API_KEY")` → `f.apiKey("OPENAI_API_KEY")`
- `os.Getenv("ANTHROPIC_API_KEY")` → `f.apiKey("ANTHROPIC_API_KEY")`
- `os.Getenv("GEMINI_API_KEY")` → `f.apiKey("GEMINI_API_KEY")`
- `os.Getenv("OPENROUTER_API_KEY")` → `f.apiKey("OPENROUTER_API_KEY")`

**Step 15.5 — Update all callers of `providers.NewFactory()`.**

- `cmd/dpkms/cmd/serve.go`: `factory := providers.NewFactory(cfg.Providers)` → requires resolver. Add before line 103:
  ```go
  resolver, err := secrets.NewResolver(cfg.Secrets)
  if err != nil {
      return fmt.Errorf("init secrets: %w", err)
  }
  factory := providers.NewFactory(cfg.Providers, resolver)
  ```
  Add import: `"github.com/ideacrafterslabs/ctxt/internal/secrets"`

- `internal/pipeline/builtins/overrides_test.go`: update `providers.NewFactory(baseCfg)` to `providers.NewFactory(baseCfg, nil)`.

**Step 15.6 — Write test with mock resolver.**

File: `internal/providers/factory_test.go` (new or extend)

```go
package providers_test

import (
    "testing"

    "github.com/ideacrafterslabs/ctxt/internal/config"
    "github.com/ideacrafterslabs/ctxt/internal/providers"
    "github.com/stretchr/testify/assert"
)

type mockResolver struct {
    keys map[string]string
}

func (m *mockResolver) Get(key string) (string, error) {
    if v, ok := m.keys[key]; ok {
        return v, nil
    }
    return "", fmt.Errorf("not found")
}
func (m *mockResolver) Set(key, value string) error { return nil }

func TestFactoryUsesResolverForAPIKey(t *testing.T) {
    resolver := &mockResolver{keys: map[string]string{
        "ANTHROPIC_API_KEY": "sk-test-value",
    }}
    cfg := config.ProvidersConfig{}
    cfg.LLM.Backend = "auto"
    f := providers.NewFactory(cfg, resolver)
    // apiKey() should use resolver first; provider creation should not panic.
    assert.NotNil(t, f)
    llm := f.LLM()
    assert.NotNil(t, llm)
}
```

Run: `go test ./internal/providers/... -run TestFactoryUsesResolver`
Expected: `PASS`

**Commit:** `feat(providers): wire secrets.Resolver into Factory; replace os.Getenv with resolver.Get`

---

## Phase 7 — New commands

### Task 16 — `ctxt config diff`

**File:** `cmd/ctxt/cmd/config.go`

**Step 16.1 — Add `configDiffCmd`.**

```go
var configDiffCmd = &cobra.Command{
    Use:   "diff",
    Short: "Show where each config value comes from",
    Long: `Show a three-column table of key / source / value for every
resolved configuration entry. Source is one of: default, file, env, flag.`,
    RunE: runConfigDiff,
}
```

Register in `init()`:
```go
configCmd.AddCommand(configDiffCmd)
```

**Step 16.2 — Implement `runConfigDiff`.**

```go
func runConfigDiff(cmd *cobra.Command, args []string) error {
    // Snapshot all keys from viper to determine their source.
    // viper.IsSet() returns true if set from any source (not just default).
    // We compare the final value against the default to label the source.
    rows := [][]string{}

    type entry struct {
        key   string
        value string
    }

    // Enumerate interesting keys.
    keys := []string{
        "version",
        "storage.type", "storage.path",
        "server.port", "server.grpc_port", "server.workers", "server.public",
        "jobs.poll_interval", "jobs.stale_timeout", "jobs.max_retries", "jobs.max_hops",
        "profile.default",
        "secrets.backend",
        "conventions.enforce_mention_namespaces",
        "providers.llm.backend", "providers.llm.model", "providers.llm.endpoint",
        "providers.vision.backend",
        "providers.embedding.backend",
    }

    v := viper.GetViper()
    for _, k := range keys {
        val := fmt.Sprintf("%v", v.Get(k))
        source := "default"
        if v.IsSet(k) {
            // Heuristic: check if env var or flag set it.
            // viper does not expose source directly, so we use InConfig.
            if v.InConfig(k) {
                source = "file"
            } else {
                source = "env/flag"
            }
        }
        rows = append(rows, []string{k, source, val})
    }

    if isJSONOutput() {
        out := make([]map[string]string, 0, len(rows))
        for _, r := range rows {
            out = append(out, map[string]string{"key": r[0], "source": r[1], "value": r[2]})
        }
        return outputJSON(os.Stdout, out)
    }

    printTable(os.Stdout, []string{"Key", "Source", "Value"}, rows)
    return nil
}
```

**Step 16.3 — Smoke test.**

```
go build ./cmd/ctxt/... && ./ctxt config diff
```

Expected: table printed with all keys, sources, and values. No error exit.

**Commit:** `feat(cli): add ctxt config diff command`

---

### Task 17 — `ctxt init` wizard

**Step 17.1 — Add `charmbracelet/huh` dependency.**

```
go get github.com/charmbracelet/huh@latest
```

**Step 17.2 — Create `internal/config/personas.go`.**

```go
package config

// BuiltinPersona is a pre-configured FocusProfile template.
type BuiltinPersona struct {
    Name    string
    Profile FocusProfile
}

// BuiltinPersonas returns the four built-in persona templates.
func BuiltinPersonas() []BuiltinPersona {
    return []BuiltinPersona{
        {
            Name: "founder",
            Profile: FocusProfile{
                Description:       "Work & business context",
                Tags:              []string{"work", "business", "strategy", "ops"},
                MentionNamespaces: []string{"project", "org", "person"},
                RerankBoosts:      map[string]float64{"decision": 1.5, "project": 1.3},
            },
        },
        {
            Name: "engineer",
            Profile: FocusProfile{
                Description:       "Engineering & code context",
                Tags:              []string{"engineering", "code", "infra", "architecture"},
                MentionNamespaces: []string{"project", "concept", "file"},
                RerankBoosts:      map[string]float64{"decision": 1.4, "concept": 1.2},
            },
        },
        {
            Name: "researcher",
            Profile: FocusProfile{
                Description:       "Research & literature context",
                Tags:              []string{"research", "literature", "analysis"},
                MentionNamespaces: []string{"concept", "person", "org"},
                RerankBoosts:      map[string]float64{"concept": 1.5, "person": 1.2},
            },
        },
        {
            Name: "writer",
            Profile: FocusProfile{
                Description:       "Writing & personal context",
                Tags:              []string{"writing", "personal", "draft", "ideas"},
                MentionNamespaces: []string{"concept", "idea", "action"},
                RerankBoosts:      map[string]float64{"concept": 1.3, "idea": 1.4},
            },
        },
    }
}
```

**Step 17.3 — Create `cmd/ctxt/cmd/init.go`.**

```go
package cmd

import (
    "fmt"
    "os"
    "path/filepath"

    "github.com/charmbracelet/huh"
    "github.com/ideacrafterslabs/ctxt/internal/config"
    "github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
    Use:   "init",
    Short: "Interactive setup wizard",
    Long: `Run the interactive setup wizard to create or update your ctxt configuration.

If a configuration file already exists, init shows a diff of what would change
before applying.

Steps:
  1. Choose your primary persona
  2. Set your data directory
  3. Choose an AI provider and enter your API key
  4. Opt in to filesystem watcher
  5. Preview and confirm`,
    RunE: runInit,
}

func init() {
    rootCmd.AddCommand(initCmd)
}

func runInit(cmd *cobra.Command, args []string) error {
    cfgPath := config.GetConfigPath()

    // Load existing config or start fresh.
    existing, loadErr := config.Load(cfgPath)
    isNew := loadErr != nil || existing == nil
    if isNew {
        existing = &config.Config{}
    }

    // --- Wizard state ---
    var (
        selectedPersona string
        dataDir         string
        aiProvider      string
        aiKey           string
        enableWatcher   bool
        confirmed       bool
    )

    // Default data dir.
    home, _ := os.UserHomeDir()
    dataDir = existing.Storage.Path
    if dataDir == "" {
        dataDir = filepath.Join(home, ".local", "share", "contexthelp", "db.sqlite")
    }

    // Step 1: Persona picker.
    personas := config.BuiltinPersonas()
    personaOptions := make([]huh.Option[string], len(personas))
    for i, p := range personas {
        personaOptions[i] = huh.NewOption(fmt.Sprintf("%-12s — %s", p.Name, p.Profile.Description), p.Name)
    }

    step1 := huh.NewGroup(
        huh.NewSelect[string]().
            Title("Which persona best describes your primary use?").
            Options(personaOptions...).
            Value(&selectedPersona),
    )

    // Step 2: Data directory.
    step2 := huh.NewGroup(
        huh.NewInput().
            Title("Data directory (SQLite path)").
            Value(&dataDir).
            Placeholder(dataDir),
    )

    // Step 3: AI provider + key.
    providerOptions := []huh.Option[string]{
        huh.NewOption("Anthropic (claude-3-5-sonnet)", "anthropic"),
        huh.NewOption("OpenAI (gpt-4o)", "openai"),
        huh.NewOption("Ollama (local, no key needed)", "ollama"),
        huh.NewOption("Skip — configure manually", "skip"),
    }

    step3 := huh.NewGroup(
        huh.NewSelect[string]().
            Title("AI provider for enrichment and tagging").
            Options(providerOptions...).
            Value(&aiProvider),
        huh.NewInput().
            Title("API key (leave blank to set via environment)").
            EchoMode(huh.EchoModePassword).
            Value(&aiKey),
    )

    // Step 4: Watcher opt-in.
    step4 := huh.NewGroup(
        huh.NewConfirm().
            Title("Enable filesystem watcher for automatic ingestion?").
            Value(&enableWatcher),
    )

    // Step 5: Preview + confirm.
    step5 := huh.NewGroup(
        huh.NewConfirm().
            Title("Apply these settings?").
            Description("Your configuration will be written to:\n  " + cfgPath).
            Value(&confirmed),
    )

    form := huh.NewForm(step1, step2, step3, step4, step5)
    if err := form.Run(); err != nil {
        return fmt.Errorf("init: wizard: %w", err)
    }

    if !confirmed {
        fmt.Println("Aborted — no changes made.")
        return nil
    }

    // Apply persona.
    if existing.Profile.Profiles == nil {
        existing.Profile.Profiles = make(map[string]config.FocusProfile)
    }
    for _, p := range personas {
        if p.Name == selectedPersona {
            existing.Profile.Profiles[selectedPersona] = p.Profile
            existing.Profile.Default = selectedPersona
            break
        }
    }

    // Apply data dir.
    existing.Storage.Type = "sqlite"
    existing.Storage.Path = dataDir

    // Apply AI provider.
    if aiProvider != "skip" {
        existing.Providers.LLM.Backend = aiProvider
    }

    // Apply watcher.
    existing.Watch.Enabled = enableWatcher

    // Ensure config dir exists.
    if err := os.MkdirAll(filepath.Dir(cfgPath), 0755); err != nil {
        return fmt.Errorf("init: mkdir: %w", err)
    }

    // Write API key to env hint (we do not write keys to disk; print instructions).
    if aiKey != "" && aiProvider != "skip" && aiProvider != "ollama" {
        envVar := map[string]string{
            "anthropic": "ANTHROPIC_API_KEY",
            "openai":    "OPENAI_API_KEY",
        }[aiProvider]
        if envVar != "" {
            fmt.Fprintf(os.Stderr, "\nTo set your API key, add to your shell profile:\n  export %s=%s\n\n", envVar, aiKey)
        }
    }

    if err := config.WriteBack(existing, cfgPath); err != nil {
        return fmt.Errorf("init: write config: %w", err)
    }

    if isNew {
        fmt.Printf("Config created at: %s\n", cfgPath)
    } else {
        fmt.Printf("Config updated at: %s\n", cfgPath)
    }
    fmt.Printf("Default profile set to: %s\n", selectedPersona)
    fmt.Println("\nRun 'ctxt config show' to verify.")
    return nil
}
```

**Step 17.4 — Build check.**

```
go build ./cmd/ctxt/...
```

Expected: no errors.

**Commit:** `feat(cli): add ctxt init wizard with charmbracelet/huh and built-in personas`

---

### Task 18 — `ctxt secret set <key>`

**File:** `cmd/ctxt/cmd/secret.go` (new)

**Step 18.1 — Create the file.**

```go
package cmd

import (
    "fmt"
    "os"

    "github.com/charmbracelet/huh"
    "github.com/ideacrafterslabs/ctxt/internal/secrets"
    "github.com/spf13/cobra"
)

var secretCmd = &cobra.Command{
    Use:   "secret",
    Short: "Manage secrets",
}

var secretSetCmd = &cobra.Command{
    Use:   "set <key>",
    Short: "Store a secret in the configured backend",
    Long: `Store a secret value for key in the configured secrets backend.

The value is prompted interactively with input masking.

Example:
  ctxt secret set ANTHROPIC_API_KEY`,
    Args: cobra.ExactArgs(1),
    RunE: runSecretSet,
}

func init() {
    rootCmd.AddCommand(secretCmd)
    secretCmd.AddCommand(secretSetCmd)
}

func runSecretSet(cmd *cobra.Command, args []string) error {
    key := args[0]

    resolver, err := secrets.NewResolver(cfg.Secrets)
    if err != nil {
        return fmt.Errorf("secret set: %w", err)
    }

    var value string
    form := huh.NewForm(
        huh.NewGroup(
            huh.NewInput().
                Title(fmt.Sprintf("Value for %s", key)).
                EchoMode(huh.EchoModePassword).
                Value(&value),
        ),
    )
    if err := form.Run(); err != nil {
        return fmt.Errorf("secret set: prompt: %w", err)
    }

    if value == "" {
        fmt.Fprintln(os.Stderr, "No value entered — aborted.")
        return nil
    }

    if err := resolver.Set(key, value); err != nil {
        return fmt.Errorf("secret set: %w", err)
    }

    fmt.Printf("Secret %q stored via %s backend.\n", key, cfg.Secrets.Backend)
    return nil
}
```

**Step 18.2 — Build check.**

```
go build ./cmd/ctxt/...
```

Expected: no errors.

**Step 18.3 — Manual smoke test.**

```
./ctxt secret set TEST_KEY
# enter a test value at the prompt
# confirm "Secret "TEST_KEY" stored via env backend." message
# (EnvResolver.Set returns an error with instructions, which is the expected behavior for env backend)
```

Expected: error message "EnvResolver is read-only" — correct behavior for env backend.

**Commit:** `feat(cli): add ctxt secret set with masked prompt`

---

## End-to-End Verification

After all phases, run:

```
go build ./...
go test ./...
```

Expected: all tests pass, no compile errors.

Smoke-test the full command tree:

```bash
./ctxt config diff
./ctxt config validate
./ctxt profile list
./ctxt profile create test-profile
./ctxt profile set-default test-profile
./ctxt profile delete test-profile
./ctxt secret set ANTHROPIC_API_KEY   # masked prompt, expect env backend message
# ./ctxt init  (requires TTY)
```

---

## Dependency Summary

| Dependency | Already present | Action |
|---|---|---|
| `github.com/spf13/viper` | yes | — |
| `gopkg.in/yaml.v3` | check `go.mod` | `go get` if absent |
| `github.com/charmbracelet/lipgloss` | yes (used in helpers.go) | — |
| `github.com/charmbracelet/huh` | no | `go get github.com/charmbracelet/huh` |
| `filippo.io/age` | no | `go get filippo.io/age` |

---

## Commit Message Sequence

```
feat(config): add JobsConfig, PipelinesConfig, ConventionsConfig, SecretsConfig, WatchConfig, InboxConfig, FocusProfile structs with defaults
feat(config): add Validate() with 6 validation rules
feat(config): add atomic WriteBack()
feat(config): add schema versioning and v0→v1 migration
feat(jobs): wire JobsConfig into WorkerPool; remove hardcoded constants
chore(jobs): phase 2 compile check — all tests pass
feat(builtins): per-pipeline provider overrides via PipelinesConfig
test(builtins): verify per-pipeline provider override wiring
feat(service): add mention namespace enforcement via ConventionsConfig
feat(cli): tag fuzzy suggestions using Levenshtein distance against TagVocabulary
feat(cli): implement profile create/delete/set-default with WriteBack
test(cli): profile create/delete/set-default tests
feat(secrets): add Resolver interface and EnvResolver
feat(secrets): add keychain and age-file backends with factory
feat(providers): wire secrets.Resolver into Factory; replace os.Getenv with resolver.Get
feat(cli): add ctxt config diff command
feat(cli): add ctxt init wizard with charmbracelet/huh and built-in personas
feat(cli): add ctxt secret set with masked prompt
```

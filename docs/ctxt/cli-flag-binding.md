# CLI Flag Binding, Precedence, and Configuration Loading

This document explains how ContextHelp binds CLI flags to configuration values, the order of precedence when the same setting is specified multiple ways, and the operational details of how Cobra and Viper work together in the `ctxt` and `dpkms` commands.

---

## Table of Contents

1. [Configuration Loading Order](#configuration-loading-order)
2. [Flag-to-Config Mapping Table](#flag-to-config-mapping-table)
3. [Environment Variable Mapping](#environment-variable-mapping)
4. [Precedence Resolution Examples](#precedence-resolution-examples)
5. [Persistent vs Command-Specific Flags](#persistent-vs-command-specific-flags)
6. [Configuration Validation](#configuration-validation)
7. [Profile-Specific Overrides](#profile-specific-overrides)

---

## Configuration Loading Order

ContextHelp uses a layered configuration system where higher layers override lower layers. The loading order is:

```
1. Defaults (compiled into binary)
   ↓
2. System config (/etc/contexthelp/config.yaml)
   ↓
3. User config (~/.config/contexthelp/config.yaml)
   ↓
4. Environment variables (CH_*, CTXT_*, DPKMS_*, OPENAI_*, ANTHROPIC_*)
   ↓
5. Runtime flags (--flag value)
```

### How It Works

When you run a command, Viper:

1. **Loads defaults** from the code
2. **Reads system config** if `/etc/contexthelp/config.yaml` exists
3. **Reads user config** from `~/.config/contexthelp/config.yaml` (or `$CTXT_CONFIG`)
4. **Applies environment variables** that match configured prefixes
5. **Binds CLI flags** via Cobra, which take precedence over everything

### Example: Value Override Chain

Given this configuration state:

```yaml
# ~/.config/contexthelp/config.yaml
ctxt:
  profiles:
    default: engineer
  cli:
    output_format: yaml
```

And these environment variables:

```bash
export CTXT_PROFILE=founder
export CH_PROFILE=research  # CH_ prefix also supported
```

And this command:

```bash
ctxt analyze "content" --profile data-scientist --output json
```

**Resolution:**

| Setting | Default | Config File | Environment | Flag | Final Value |
|---------|---------|-------------|-------------|------|-------------|
| profile | `general` | `engineer` | `research` | `data-scientist` | `data-scientist` |
| output | `text` | `yaml` | _(not set)_ | `json` | `json` |

**Result:** The command uses the `data-scientist` profile with `json` output format.

---

## Flag-to-Config Mapping Table

This table shows how CLI flags map to Viper configuration keys. Viper uses dot notation (e.g., `ctxt.profiles.default`) to access nested YAML structures.

### Global Persistent Flags (Available on all commands)

| Short Flag | Long Flag | Viper Config Key | Type | Description |
|------------|-----------|-----------------|------|-------------|
| `-c` | `--config` | _(special)_ | string | Path to config file (bypasses default search) |
| | `--profile` | `ctxt.profiles.default` | string | Active focus profile |
| `-v` | `--verbose` | `ctxt.cli.verbose` | bool | Enable verbose output |
| | `--output` | `ctxt.cli.output_format` | string | Output format (text, json, yaml) |
| | `--color` | `ctxt.cli.color` | string | Color mode (auto, always, never) |
| | `--no-color` | `ctxt.cli.color` | bool | Disable color (sets color=never) |

### `ctxt analyze` Command Flags

| Short Flag | Long Flag | Viper Config Key | Type | Description |
|------------|-----------|-----------------|------|-------------|
| | `--type` | _(runtime only)_ | string | Input type override (text, url, image, etc.) |
| | `--hints` | _(runtime only)_ | string | Semantic hints for tagging |
| | `--mentions` | _(runtime only)_ | string | Explicit entity mentions |
| `-f` | `--file` | _(runtime only)_ | string | Read input from file |
| | `--pipeline` | `ctxt.pipelines.default_timeout` | string | Force specific pipeline |
| | `--lang` | `ctxt.i18n.preferred_languages[0]` | string | Input language override |
| | `--translate` | `ctxt.i18n.auto_translate` | string | Translation mode (none/auto) |
| | `--raw` | _(runtime only)_ | bool | Disable AI processing |
| | `--wait` | _(runtime only)_ | bool | Block until job completes |

### `ctxt list` Command Flags

| Short Flag | Long Flag | Viper Config Key | Type | Description |
|------------|-----------|-----------------|------|-------------|
| | `--type` | _(filter only)_ | string | Filter by object type |
| | `--tag` | _(filter only)_ | string | Filter by tags |
| | `--hint` | _(filter only)_ | string | Filter by hints |
| | `--mention` | _(filter only)_ | string | Filter by mention |
| | `--pipeline` | _(filter only)_ | string | Filter by pipeline |
| | `--before` | _(filter only)_ | string | Created before date |
| | `--after` | _(filter only)_ | string | Created after date |
| `-l` | `--limit` | `dpkms.query.default_limit` | int | Max results per page |
| | `--start` | _(pagination only)_ | int | Pagination offset |
| | `--sort` | _(runtime only)_ | string | Sort mode (recent, match, weight) |
| | `--no-track` | _(runtime only)_ | bool | Skip match tracking |

### `ctxt find` Command Flags

| Short Flag | Long Flag | Viper Config Key | Type | Description |
|------------|-----------|-----------------|------|-------------|
| | `--profile` | `ctxt.profiles.default` | string | Use focus profile |
| `-l` | `--limit` | `ctxt.search.default_limit` | int | Max search results |
| | `--json` | `ctxt.cli.output_format` | bool | JSON output (sets format=json) |
| | `--yaml` | `ctxt.cli.output_format` | bool | YAML output (sets format=yaml) |

### `ctxt make` Command Flags

| Short Flag | Long Flag | Viper Config Key | Type | Description |
|------------|-----------|-----------------|------|-------------|
| | `--profile` | `ctxt.profiles.default` | string | Use focus profile |
| | `--mention` | _(filter only)_ | string | Focus on specific mentions |
| | `--tag` | _(filter only)_ | string | Focus on specific tags |
| | `--since` | _(filter only)_ | string | Include knowledge since date |
| `-o` | `--output` | _(runtime only)_ | string | Write to file (different from format) |

### `dpkms serve` Command Flags

| Short Flag | Long Flag | Viper Config Key | Type | Description |
|------------|-----------|-----------------|------|-------------|
| `-p` | `--port` | `dpkms.api.rest.port` | int | HTTP API port |
| | `--grpc-port` | `dpkms.api.grpc.port` | int | gRPC API port |
| | `--profile` | `ctxt.profiles.default` | string | Default focus profile |
| | `--public` | `dpkms.api.rest.public` | bool | Allow remote connections |
| `-c` | `--config` | _(special)_ | string | Custom config file path |
| `-w` | `--workers` | `dpkms.jobs.workers` | int | Number of worker threads |

### Flag Binding Implementation

In Cobra/Viper, flags are bound like this:

```go
// Persistent flag (available on all commands)
rootCmd.PersistentFlags().StringP("profile", "", "", "Active focus profile")
viper.BindPFlag("ctxt.profiles.default", rootCmd.PersistentFlags().Lookup("profile"))

// Command-specific flag
analyzeCmd.Flags().StringP("hints", "", "", "Semantic hints for tagging")
// Note: Not bound to viper, accessed via cmd.Flags().GetString("hints")

// Flag with default from config
listCmd.Flags().IntP("limit", "l", viper.GetInt("dpkms.query.default_limit"), "Max results")
viper.BindPFlag("dpkms.query.default_limit", listCmd.Flags().Lookup("limit"))
```

---

## Environment Variable Mapping

ContextHelp reads environment variables with specific prefixes. Viper automatically maps these to config keys.

### Prefix Conventions

| Prefix | Scope | Example |
|--------|-------|---------|
| `CH_` | Cross-cutting (both dPKMS and ctxt) | `CH_STORAGE_TYPE`, `CH_WORKERS` |
| `CTXT_` | ctxt-specific | `CTXT_PROFILE`, `CTXT_DATA_DIR` |
| `DPKMS_` | dPKMS-specific | `DPKMS_WORKERS`, `DPKMS_DATA_DIR` |
| `OPENAI_` | AI provider | `OPENAI_API_KEY` |
| `ANTHROPIC_` | AI provider | `ANTHROPIC_API_KEY` |

### Environment Variable Mapping Rules

Viper converts environment variables to config keys using these rules:

1. Strip the prefix (`CH_`, `CTXT_`, `DPKMS_`)
2. Convert to lowercase
3. Replace `_` with `.` to create nested paths
4. Replace `__` with `_` for actual underscores in keys

### Common Environment Variables

#### Cross-Cutting (CH_)

```bash
# Storage
CH_STORAGE_TYPE=sqlite              # → dpkms.storage.backend
CH_STORAGE_PATH=/custom/path        # → dpkms.storage.sqlite.path

# Jobs
CH_WORKERS=8                        # → dpkms.jobs.workers
CH_RETRY_LIMIT=5                    # → dpkms.jobs.retry_limit

# HTTP API
CH_HTTP_PORT=8080                   # → dpkms.api.rest.port
CH_GRPC_PORT=9090                   # → dpkms.api.grpc.port

# Privacy
CH_DISABLE_TELEMETRY=true           # → monitoring.telemetry.enabled (inverted)

# Registry authentication
CH_REGISTRY_TOKEN_UXPATTERNS=abc123 # → dpkms.registries.subscriptions[name=uxpatterns].auth_token
```

#### ctxt-Specific (CTXT_)

```bash
# Configuration
CTXT_CONFIG=/path/to/config.yaml    # Override config file location
CTXT_DATA_DIR=/custom/data          # → ctxt.data_dir

# Profile
CTXT_PROFILE=founder                # → ctxt.profiles.default

# AI
OPENAI_API_KEY=sk-...               # → ctxt.ai.providers.openai.api_key
ANTHROPIC_API_KEY=sk-...            # → ctxt.ai.providers.anthropic.api_key

# Pipelines
CTXT_DEFAULT_PIPELINE=text.short    # → ctxt.pipelines.defaults.text

# Language
CTXT_LANG=fr,en                     # → ctxt.i18n.preferred_languages
CTXT_AUTO_TRANSLATE=false           # → ctxt.i18n.auto_translate
```

#### dPKMS-Specific (DPKMS_)

```bash
# Configuration
DPKMS_CONFIG=/path/to/config.yaml   # Override config file location
DPKMS_DATA_DIR=/custom/data         # → dpkms.data_dir

# Workers
DPKMS_WORKERS=12                    # → dpkms.jobs.workers
DPKMS_POLL_INTERVAL=2s              # → dpkms.jobs.poll_interval

# Query
DPKMS_MAX_RESULTS=200               # → dpkms.query.max_results
DPKMS_QUERY_TIMEOUT=60s             # → dpkms.query.timeout
```

### Special Environment Variables

Some environment variables have special handling:

```bash
# Config file override (highest priority for file location)
CTXT_CONFIG=/custom/config.yaml     # Used before default search paths

# Data directory override
CTXT_DATA_DIR=/custom/data          # Overrides all path resolutions
DPKMS_DATA_DIR=/custom/data         # dPKMS variant

# Hard disable flags
CH_DISABLE_TELEMETRY=1              # Disables telemetry regardless of config
```

---

## Precedence Resolution Examples

These examples show how values are resolved when set multiple ways.

### Example 1: Profile Selection

**Scenario:** Profile set in config, environment, and flag.

```yaml
# config.yaml
ctxt:
  profiles:
    default: engineer
```

```bash
export CTXT_PROFILE=founder
ctxt analyze "content" --profile research
```

**Resolution Steps:**

1. Default: `general` (compiled default)
2. Config: `engineer` (from config.yaml)
3. Environment: `founder` (from CTXT_PROFILE)
4. Flag: `research` (from --profile)

**Result:** Uses `research` profile.

### Example 2: Output Format

**Scenario:** Format set via flag shorthand.

```yaml
# config.yaml
ctxt:
  cli:
    output_format: yaml
```

```bash
ctxt list --json
```

**Resolution Steps:**

1. Default: `text`
2. Config: `yaml`
3. Flag: `json` (--json is shorthand for --output json)

**Result:** Outputs in JSON format.

### Example 3: Worker Count

**Scenario:** Workers set in multiple places.

```yaml
# config.yaml
dpkms:
  jobs:
    workers: 4
```

```bash
export CH_WORKERS=8
dpkms serve --workers 12
```

**Resolution Steps:**

1. Default: `4` (compiled default)
2. Config: `4` (from config.yaml)
3. Environment: `8` (from CH_WORKERS)
4. Flag: `12` (from --workers)

**Result:** Serves with 12 workers.

### Example 4: Limit Flag with Config Default

**Scenario:** Limit not specified, uses config default.

```yaml
# config.yaml
dpkms:
  query:
    default_limit: 50
```

```bash
ctxt list --type url
```

**Resolution Steps:**

1. Default: `20` (compiled default)
2. Config: `50` (from config.yaml)
3. Flag: _(not provided)_

**Result:** Returns 50 results.

### Example 5: Multiple Flag Sources

**Scenario:** Same logical value from different flag names.

```bash
ctxt list --output json  # Explicit format flag
ctxt list --json         # Shorthand flag
```

Both commands produce identical output. The `--json` flag is a boolean shorthand that sets `output_format=json`.

### Example 6: Profile-Specific Registry Selection

**Scenario:** Profile overrides registry selection.

```yaml
# config.yaml
dpkms:
  registries:
    subscriptions:
      - name: default-taxonomy
        enabled: true
      - name: uxpatterns
        enabled: false

ctxt:
  profiles:
    founder:
      registries:
        - uxpatterns  # Founder profile enables uxpatterns
```

```bash
ctxt analyze "content" --profile founder
```

**Resolution:**
- Default registries: `[default-taxonomy]`
- Profile override: Adds `uxpatterns` to active registries
- Result: Uses both `[default-taxonomy, uxpatterns]`

---

## Persistent vs Command-Specific Flags

### Persistent Flags

These flags are available on **all commands** and are bound to Viper config keys.

| Flag | Available On | Binding |
|------|--------------|---------|
| `--config` | All commands | Special (file path) |
| `--profile` | All commands | `ctxt.profiles.default` |
| `--verbose` | All commands | `ctxt.cli.verbose` |
| `--output` | All commands | `ctxt.cli.output_format` |
| `--color` | All commands | `ctxt.cli.color` |

**Implementation:**

```go
// In cmd/root.go
rootCmd.PersistentFlags().StringP("profile", "", "", "Active focus profile")
viper.BindPFlag("ctxt.profiles.default", rootCmd.PersistentFlags().Lookup("profile"))
```

**Usage:**

```bash
# These all work
ctxt --profile founder analyze "content"
ctxt analyze --profile founder "content"
ctxt find --profile founder "query"
ctxt list --profile founder
```

### Command-Specific Flags

These flags are only available on specific commands and are **not** bound to Viper.

#### `ctxt analyze` Only

| Flag | Purpose | Access Method |
|------|---------|---------------|
| `--hints` | Semantic hints | `cmd.Flags().GetString("hints")` |
| `--mentions` | Explicit mentions | `cmd.Flags().GetString("mentions")` |
| `--type` | Input type override | `cmd.Flags().GetString("type")` |
| `--file` | Input from file | `cmd.Flags().GetString("file")` |
| `--raw` | Disable AI | `cmd.Flags().GetBool("raw")` |
| `--wait` | Synchronous mode | `cmd.Flags().GetBool("wait")` |

**Why not bound to Viper?**
- These flags are operation-specific parameters, not configuration
- They don't have meaningful defaults in config files
- They represent per-invocation behavior, not persistent preferences

#### `ctxt list` Only

| Flag | Purpose | Access Method |
|------|---------|---------------|
| `--tag` | Filter by tag | `cmd.Flags().GetString("tag")` |
| `--hint` | Filter by hint | `cmd.Flags().GetString("hint")` |
| `--mention` | Filter by mention | `cmd.Flags().GetString("mention")` |
| `--before` | Date filter | `cmd.Flags().GetString("before")` |
| `--after` | Date filter | `cmd.Flags().GetString("after")` |
| `--sort` | Sort mode | `cmd.Flags().GetString("sort")` |
| `--no-track` | Skip tracking | `cmd.Flags().GetBool("no-track")` |

#### `dpkms serve` Only

| Flag | Purpose | Binding |
|------|---------|---------|
| `--port` | HTTP port | `dpkms.api.rest.port` |
| `--grpc-port` | gRPC port | `dpkms.api.grpc.port` |
| `--workers` | Worker count | `dpkms.jobs.workers` |
| `--public` | Remote access | `dpkms.api.rest.public` |

**Note:** `dpkms serve` flags **are** bound to Viper because they override operational configuration.

---

## Configuration Validation

### What Happens When Flags Conflict with Config

ContextHelp validates configuration after merging all layers.

#### Validation Checks

1. **Schema validation** - YAML structure matches expected schema
2. **Type validation** - Values are correct types (int, string, bool)
3. **Range validation** - Numeric values within acceptable ranges
4. **Dependency validation** - Required related settings present
5. **Plugin validation** - Plugins load successfully with provided config

#### Validation Commands

```bash
# Validate configuration (reports errors)
ctxt config validate

# Show resolved configuration (after all merges)
ctxt config show

# Show resolved configuration for specific key
ctxt config show ctxt.profiles

# Test specific flag binding
ctxt --profile founder config show ctxt.profiles.default
# Output: founder
```

### Required vs Optional Values

| Setting | Required? | Validation |
|---------|-----------|------------|
| `dpkms.storage.backend` | Yes | Must be valid backend (sqlite, postgres, leann) |
| `dpkms.storage.sqlite.path` | Yes (if sqlite) | Must be writable path |
| `ctxt.profiles.default` | No | Defaults to `general` |
| `ctxt.ai.providers.openai.api_key` | No | Required only if OpenAI provider used |
| `--profile` flag | No | Falls back to config or default |
| `--type` flag on analyze | No | Auto-detected if not provided |

### Type Validation

```bash
# Valid - port is integer
dpkms serve --port 8080

# Invalid - port must be integer
dpkms serve --port abc
# Error: invalid argument "abc" for "--port" flag: strconv.ParseInt: parsing "abc": invalid syntax

# Valid - workers is integer
export CH_WORKERS=8

# Invalid - workers must be positive
export CH_WORKERS=-1
# Error: dpkms.jobs.workers must be positive integer, got -1
```

### Example: Invalid Configuration

```yaml
# config.yaml
dpkms:
  storage:
    backend: invalid_backend  # Not a valid backend
  jobs:
    workers: -5              # Must be positive
```

```bash
ctxt config validate
# Output:
# ✗ dpkms.storage.backend: invalid value "invalid_backend", must be one of: sqlite, postgres, leann
# ✗ dpkms.jobs.workers: must be positive integer, got -5
# Config validation failed with 2 errors
```

---

## Profile-Specific Overrides

Profiles provide a powerful way to override configuration at runtime.

### How Profiles Work

Profiles are defined in `ctxt.profiles.<name>` and can override:
- Active registries
- Pipeline selection
- Tag filters and boosts
- Language preferences
- Reranking weights

### Profile Definition Example

```yaml
# config.yaml
ctxt:
  profiles:
    default: engineer  # Default profile if not specified

    engineer:
      registries:
        - local-taxonomy
      pipelines: ["*"]  # All pipelines enabled
      tags:
        include: ["technical.*", "code.*"]
        boost: ["architecture.*", "performance.*"]

    founder:
      registries:
        - uxpatterns
        - growth-weights
      pipelines:
        - text.long
        - url.generic
      tags:
        include: ["business.*", "growth.*"]
        exclude: ["technical.*"]

    research:
      registries:
        - local-taxonomy
        - uxpatterns
      pipelines: ["*"]
      search:
        vector:
          enabled: true
          weight: 0.5
```

### Profile Override Examples

#### Example 1: Profile Changes Registry Selection

```bash
# Without profile (uses default: engineer)
ctxt find "onboarding flow"
# Searches: local-taxonomy only

# With founder profile
ctxt find "onboarding flow" --profile founder
# Searches: uxpatterns + growth-weights
```

#### Example 2: Profile Changes Pipeline Selection

```bash
# Engineer profile (all pipelines)
ctxt analyze screenshot.png --profile engineer
# Uses: image.ocr pipeline

# Founder profile (limited pipelines)
ctxt analyze screenshot.png --profile founder
# Error: image.ocr not enabled in founder profile
```

#### Example 3: Profile Changes Search Ranking

```yaml
# research profile definition
research:
  search:
    ranking_weights:
      recency: 0.2
      relevance: 0.6  # Higher relevance weight
      frequency: 0.2
```

```bash
# Default ranking
ctxt find "API design"
# Results ranked: recency=0.3, relevance=0.5, frequency=0.2

# Research profile ranking
ctxt find "API design" --profile research
# Results ranked: recency=0.2, relevance=0.6, frequency=0.2
# (More weight on content relevance vs recency)
```

#### Example 4: Profile Persistent Selection

```bash
# Set default profile for session
ctxt profile use founder

# All subsequent commands use founder profile
ctxt analyze "content"      # Uses founder profile
ctxt find "query"           # Uses founder profile

# Override per-command
ctxt find "query" --profile engineer

# Reset to default
ctxt profile use engineer
```

### Profile Override Precedence

When using profiles, the precedence chain becomes:

```
1. Compiled defaults
   ↓
2. Global config (ctxt.*)
   ↓
3. Profile config (ctxt.profiles.<name>.*)
   ↓
4. Environment variables
   ↓
5. Runtime flags
```

**Example:**

```yaml
# config.yaml
dpkms:
  registries:
    subscriptions:
      - name: default-taxonomy
        enabled: true

ctxt:
  profiles:
    engineer:
      registries:
        - local-taxonomy
        - custom-tech-registry
```

```bash
export CTXT_REGISTRIES=uxpatterns  # Environment override

ctxt analyze "content" --profile engineer
```

**Resolution:**
1. Default registries: `[default-taxonomy]`
2. Profile override: `[local-taxonomy, custom-tech-registry]`
3. Environment override: `[uxpatterns]`
4. Final: Uses `uxpatterns` (environment wins)

---

## Summary

### Key Takeaways

1. **Precedence Chain:** Defaults → System Config → User Config → Environment → Flags
2. **Persistent Flags:** Available on all commands (--profile, --output, --verbose)
3. **Command-Specific Flags:** Only available on specific commands (--hints, --limit)
4. **Environment Prefixes:** `CH_` (cross-cutting), `CTXT_` (ctxt), `DPKMS_` (dpkms)
5. **Profiles Override:** Profiles apply between global config and environment
6. **Flag Binding:** Cobra flags bound to Viper config keys for persistent settings
7. **Validation:** Use `ctxt config validate` and `ctxt config show` to debug

### Quick Reference Commands

```bash
# Show resolved configuration
ctxt config show

# Test flag binding
ctxt --profile founder config show ctxt.profiles.default

# Validate configuration
ctxt config validate

# Check precedence
ctxt analyze "test" --profile engineer --verbose
# Uses: engineer profile, verbose output, all config overrides applied

# Debug environment variables
env | grep -E '^(CH_|CTXT_|DPKMS_)'
```

### Related Documentation

- [configuration.md](./configuration.md) - Full configuration reference
- [api-cli.md](./api-cli.md) - Complete CLI command reference
- [../configuration-structure.md](../configuration-structure.md) - Config file organization
- [../design.md](../design.md) - System architecture overview

---

**Document Version:** 1.0
**Last Updated:** 2026-01-26

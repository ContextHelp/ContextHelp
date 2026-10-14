# Configuration Structure for ContextHelp

This document defines how configuration is organized across the dPKMS (substrate) and ctxt (brain) packages.

---

## Configuration File Location

**Primary config file:**
```
~/.config/contexthelp/config.yaml
```

Both `dpkms` and `ctxt` binaries read from this single file but consume different sections.

---

## Full Configuration Example

```yaml
#############################################################################
# dPKMS Configuration (Substrate)
#############################################################################

storage:
  type: sqlite  # sqlite | postgres | leann | plugin-<name>
  path: ~/.local/share/contexthelp/data.db
  concurrency: safe
  options: {}

jobs:
  workers: 4
  retry_limit: 3
  max_runtime_seconds: 600

queue:
  backend: sqlite  # sqlite | redis | memory | plugin-<name>
  options: {}

registries:
  enabled:
    - name: local-taxonomy
      url: file://~/.config/contexthelp/taxonomy/
      type: taxonomy

    - name: uxpatterns
      url: https://api.uxpatterns.io
      type: taxonomy
      auth:
        token: <uxpatterns-token>   # literal: config files do not expand env vars

    - name: growth-weights
      url: https://weights.growth.dev
      type: weights

server:
  http:
    port: 7700
    public: false
  grpc:
    port: 7701

security:
  telemetry: false
  allow_external_pipelines: false

refresh:
  enabled: true
  min_interval_seconds: 300
  default_interval_seconds: 86400

#############################################################################
# ctxt Configuration (Brain)
#############################################################################

ai_providers:
  openai:
    # API key: OPENAI_API_KEY in the environment (config files do not expand env vars)
    model: gpt-4o

  anthropic:
    # API key: ANTHROPIC_API_KEY in the environment
    model: claude-sonnet-4

  local:
    ollama_url: http://localhost:11434
    model: llama3

pipelines:
  enabled:
    - text.short
    - text.long
    - url.generic
    - url.repo
    - image.ocr
    - audio.transcript

  defaults:
    text: text.long
    url: url.generic
    image: image.ocr

profiles:
  founder:
    default: true           # mark as server default (at most one may be true)
    registries: [uxpatterns, growth-weights]
    weights: [growth-weights]
    pipelines: [text.long, url.generic]
    tags:
      include: ["business.*", "growth.*"]
      exclude: ["technical.*"]

  engineer:
    registries: [local-taxonomy]
    pipelines: ["*"]
    tags:
      include: ["technical.*", "code.*"]
      boost: ["architecture.*", "performance.*"]

  research:
    registries: [local-taxonomy, uxpatterns]
    pipelines: ["*"]
    search_strategy:
      mode: vector          # override default_mode for this profile
      rrf:
        fts_weight: 0.2
        vector_weight: 0.8

search:
  default_mode: hybrid      # "fts" | "vector" | "hybrid" (default: hybrid)
  rrf:
    k: 60                   # RRF rank constant (default: 60)
    fts_weight: 0.5         # weight for FTS leg (default: 0.5)
    vector_weight: 0.5      # weight for vector leg (default: 0.5)
  candidate_pool:
    fts: 50                 # candidates fetched from FTS leg (default: 50)
    vector: 50              # candidates fetched from vector leg (default: 50)
  min_score: 0.0            # discard results below this RRF score (default: 0.0 = off)
  fallback_to_fts: true     # degrade to FTS-only when embedding provider unavailable

i18n:
  enabled: true
  preferred_languages: ["en", "fr"]
  auto_translate: false
  translate_summaries: true

#############################################################################
# Plugin Configuration (Cross-Cutting)
#############################################################################

plugins:
  load:
    - name: alias-plugin
      config:
        definitions:
          urls: "list --type url"
          oss-inbox: "analyze --type text --hints '#OSS #inbox'"

    - name: rss-feed
      config:
        default_interval_seconds: 3600
        default_item_pipeline: text.long

    - name: notifications
      config:
        enabled: true
        max_history: 5000
        channels:
          cli:
            - levels: ["info","warning","error"]
          webhook:
            - url: "https://hooks.example.com/context"
              levels: ["update","info"]
```

---

## Configuration Loading Order

Each binary reads its own file: `ctxt` reads `ctxt.yaml`, `dpkms` reads
`dpkms.yaml`. Highest wins:

1. **Command flag**, when passed, on commands that prefer it over config
   (e.g. `dpkms serve --port 8181`)
2. **`-c key=value` overrides** (e.g. `-c server.port=8181`)
3. **Bound environment variables** (e.g. `CH_SERVER_PORT=8181`)
4. **`-c <path>` files**, later paths win
5. **Project config** (nearest `.contexthelp/<bin>.yaml` walking up from cwd)
6. **User config** (`$XDG_CONFIG_HOME/contexthelp/<bin>.yaml`)
7. **System config** (`/etc/contexthelp/<bin>.yaml`)
8. **Defaults** (compiled into binary)

`CTXT_CONFIG=<path>` replaces slots 5–7 with that one file.

---

## Environment Variable Overrides

Only an explicit list of variables maps to config keys; there is no
automatic `CH_<KEY>` translation. Common ones:

```bash
CH_STORAGE_TYPE=sqlite          # storage.type
CH_SERVER_PORT=8080             # server.port
CH_GRPC_PORT=9090               # server.grpc_port
CH_COOKIE_BRIDGE_PORT=9377      # server.cookie_bridge_port
CTXT_PROFILE=founder            # profile.default
CH_DISABLE_TELEMETRY=true       # Hard-disable telemetry (not a config key)
```

Full binding table and precedence examples:
[ctxt/cli-flag-binding.md](ctxt/cli-flag-binding.md).

---

## Config Validation

### Validate Configuration

```bash
# Validate full config
ctxt config validate

# Show resolved config (after all overrides)
ctxt config show

# Show dPKMS-specific config
dpkms config show
```

### Validation Checks

Both binaries validate:
- Schema structure (YAML syntax)
- Registry availability
- Plugin load success
- Pipeline discovery
- Profile consistency
- Refresh rules
- Cross-package compatibility

---

## Package-Specific Config Sections

### dPKMS Reads

- `storage.*`
- `jobs.*`
- `queue.*`
- `registries.*`
- `server.*`
- `security.*`
- `refresh.*`
- Plugin configs (for dPKMS plugins)

### ctxt Reads

- `ai_providers.*`
- `pipelines.*`
- `profiles.*`
- `i18n.*`
- Plugin configs (for ctxt plugins)

### Both Read

- `plugins.load` (both packages may load different plugins)

---

## Per-Profile Overrides

Profiles (defined in ctxt config) can override:
- Active registries
- Pipeline selection
- Tag filters and boosts
- Language preferences
- Reranking weights

**Example:**
```bash
# Use founder profile
ctxt --profile founder analyze "competitive analysis doc"

# Switch profile
ctxt profile use engineer
```

---

## Config File Organization Best Practices

1. **Separate sections clearly** with comments
2. **Use environment variables** for secrets
3. **Keep profiles minimal** - only override what's needed
4. **Document custom plugins** inline
5. **Version your config** in git (excluding secrets)

---

## Upgrading

Config files carry no schema version and are never migrated or rewritten
on load. Only commands that change configuration on request (`ctxt setup`,
`ctxt profile create|rm|set|unset`, `ctxt profile schema ...`,
`ctxt registry delete`, `ctxt watch enable|disable`) write a file, and each
changes only the keys it is about:

- **Which file**: the last `-c <path>` when one is given, else the user
  file (`$CTXT_CONFIG`, or `$XDG_CONFIG_HOME/contexthelp/ctxt.yaml`).
  `ctxt setup` always writes the user file. The project
  `.contexthelp/ctxt.yaml` is never written.
- **What lands in it**: the edit alone. Values from other files,
  `-c key=value`, environment variables and built-in defaults stay out.
  Comments, key order, quoting and blank lines are kept.
- **Entries from another file**: a profile or registry that another config
  file defines can't be removed or edited through this one. The command
  fails, names the file it writes, and changes nothing; edit the file that
  defines the entry.
- **Missing file**: created holding only the keys the command sets.

When upgrading ContextHelp:

1. Backup current config:
   ```bash
   dpkms housekeeping backup --include-config
   ```

2. Validate after upgrade:
   ```bash
   ctxt config validate
   dpkms config validate
   ```

---

## Summary

| Section | Owner | Purpose |
|---------|-------|---------|
| `storage` | dPKMS | Where and how data is stored |
| `jobs` | dPKMS | Job queue behavior |
| `queue` | dPKMS | Queue backend selection |
| `registries` | dPKMS | External knowledge sources |
| `server` | dPKMS | API server configuration |
| `security` | dPKMS | Privacy and encryption |
| `refresh` | dPKMS | Refresh job scheduling |
| `ai_providers` | ctxt | LLM and AI integrations |
| `pipelines` | ctxt | Pipeline definitions and defaults |
| `profiles` | ctxt | User/role-based lenses |
| `i18n` | ctxt | Localization preferences |
| `plugins` | Both | Plugin loading and configuration |

This structure ensures clean separation while maintaining a single source of configuration truth.

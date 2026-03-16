# Configuration

ContextHelp uses a declarative, polymorphic, plugin-extensible configuration system that controls every aspect of the engine: storage backends, ingestion & queue behavior, registries, pipelines, agents, server interfaces, privacy rules, localization preferences, and plugin-provided configuration types. Configuration files may be written in **YAML** or **JSON**, merged with environment variables and runtime flags.

The configuration system is designed to be:

- Deterministic
- Layered
- Declarative
- Extensible
- Plugin-friendly
- Local-first
- Backward-compatible via schema versioning

Plugins may introduce entirely new configuration blocks without requiring core changes.

---

# Configuration Layers

ContextHelp merges configuration from five ordered layers:

1. **Defaults** (compiled into the engine)
2. **System-level config** (e.g., `/etc/contexthelp/config.yaml`)
3. **User-level config** (e.g., `~/.config/contexthelp/config.yaml`)
4. **Environment variables** (e.g., `CH_STORAGE_BACKEND=sqlite`)
5. **Runtime flags** (e.g., `ch analyze --lang fr`)

Higher layers override all lower layers. Plugins may also define new configuration namespaces, merged in the same hierarchy.

---

# Example Configuration File

```yaml
version: 1

storage:
  type: sqlite
  path: ~/.local/share/contexthelp/bookmarks.db
  concurrency: safe
  options: {}

ingestion:
  mode: jobs
  auto_start_worker: true

queue:
  backend: sqlite
  workers: 4
  retry_limit: 3
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
        token: ${CH_REGISTRY_TOKEN_UXPATTERNS}

    - name: growth-weights
      url: https://weights.growth.dev
      type: weights

pipelines:
  enabled:
    - text.short
    - text.long
    - url.generic
    - url.repo
    - image.ui
    - image.landing
  defaults:
    text: text.long
    url: url.generic
    image: image.landing

agents:
  ux_critic:
    registries: [local-taxonomy, uxpatterns]
    weights: [growth-weights]
    pipelines: [image.landing, text.long]
    tags:
      include: ["ui.*"]
      exclude: ["growth.*"]

  research:
    registries: [local-taxonomy]
    pipelines: ["*"]
    search_strategy:
      mode: vector
      rrf:
        fts_weight: 0.2
        vector_weight: 0.8

search:
  default_mode: hybrid
  rrf:
    k: 60
    fts_weight: 0.5
    vector_weight: 0.5
  candidate_pool:
    fts: 50
    vector: 50
  min_score: 0.0
  fallback_to_fts: true

server:
  http:
    port: 7700
    public: false
  grpc:
    port: 7701

i18n:
  enabled: true
  preferred_languages: ["fr", "en"]
  auto_translate: true
  skip_on_analyze_default: false
  translate_summaries: true
  translate_sections: true
  translate_tags: true
  provider: openai

privacy:
  telemetry: false
  allow_external_pipelines: false

refresh:
  enabled: true
  min_interval_seconds: 300
  default_interval_seconds: 86400
  rules:
    - match: "github.com/.+"
      interval_seconds: 43200
      fetch_new: false

    - match: "youtube.com/channel/.+"
      interval_seconds: 3600
      fetch_new: true
      item_pipeline: url.generic

    - match: "feeds[.]example[.]com/.+"
      interval_seconds: 7200
      fetch_new: true

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
        rules:
          - type_match: "price_drop"
            channels: ["cli","webhook"]

    - name: price-monitor
      config:
        default_threshold_percent: 5
        patterns:
          - match: "amazon[.]com/.+"
            extract_regex: "\\$([0-9]+[.][0-9]{2})"
```

---

# Top-Level Keys

## `version`

Schema version of the configuration file. Used for compatibility and migrations.

---

# Storage Configuration

Defines where and how bookmarks and jobs are persisted.

```yaml
storage:
  type: sqlite | postgres | json | leann | plugin-<name>
  path: <path-or-connection-string>
  concurrency: safe | local
  options: {}
```

## LEANN Storage Configuration

LEANN provides graph-based RAG with 97% storage efficiency. Recommended for large corpora or storage-constrained environments.

```yaml
storage:
  type: leann
  path: ~/.local/share/contexthelp/leann
  concurrency: safe
  options:
    # LEANN-specific options
    recompute: true              # Enable on-demand embedding generation
    graph_degree: 32             # Graph degree for index construction
    build_complexity: 64          # Build complexity for index
    compact: true                # Use compact storage (CSR format)
    backend: hnsw                # or diskann for larger indices

    # ContextHelp integration
    metadata_fields:
      - tags
      - mentions
      - hints
      - decisions
      - pipeline
```

**Hybrid storage (LEANN + SQLite):**

```yaml
storage:
  # Primary: SQLite for bookmarks, entities, backlinks
  type: sqlite
  path: ~/.local/share/contexthelp/bookmarks.db
  concurrency: safe

# Vector backend: LEANN for similarity search
vector_backend:
  type: leann
  path: ~/.local/share/contexthelp/leann-vector
  options:
    recompute: true
    backend: hnsw

# Query behavior
query:
  vector_provider: leann
  merge_strategy: weighted  # or rrf, reciprocal_rank_fusion
  weights:
    metadata: 0.3
    mentions: 0.4
    vector_similarity: 0.3
```

See [LEANN Integration Documentation](../integrations/leann-integration.md) for complete details.

### Backends

| Type | Description |
|------|-------------|
| `json` | Simple, portable, single-user. Not concurrency-safe. |
| `sqlite` | Recommended default. WAL mode for safe concurrent reads. |
| `postgres` | For advanced multi-user or server deployments. |
| `leann` | Graph-based RAG with 97% storage efficiency. See [LEANN Integration](../integrations/leann-integration.md). |
| `plugin-<name>` | Storage provided by a plugin (remote KV, vector DB, etc.). |

Plugins can register new backend types dynamically.

---

# Ingestion Configuration

Controls how incoming work (e.g., `ch analyze`) is persisted and executed.

```yaml
ingestion:
  mode: jobs | inline
  auto_start_worker: true
```

### Modes

- `jobs` → transactional outbox pattern
- `inline` → synchronous execution

### Worker Behavior

When `auto_start_worker: true`, `ch serve` begins processing jobs.

---

# Queue Configuration

Defines how jobs are scheduled and retried.

```yaml
queue:
  backend: sqlite | memory | redis | plugin-<name>
  workers: <int>
  retry_limit: <int>
  options: {}
```

Plugins may define and register queue backends.

---

# Registry Configuration

External sources of taxonomy, weights, or bookmarks.

```yaml
registries:
  enabled:
    - name: <id>
      url: <endpoint-or-path>
      type: taxonomy | weights | bookmarks
      auth:
        token: <env-or-literal>
```

---

# Pipeline Configuration

Controls pipeline availability and routing.

```yaml
pipelines:
  enabled:
    - text.long
    - url.generic
  defaults:
    text: text.long
    url: url.generic
```

Plugins may add new pipelines dynamically (e.g., `feed.fetch`, `rss.parse`).

---

# Agent Profiles

Define scoped semantic and retrieval worldviews.

```yaml
agents:
  myagent:
    registries: [uxpatterns]
    pipelines: ["text.long"]
    tags:
      include: ["ui.*"]
      exclude: ["nsfw.*"]
```

---

# Server Configuration

Defines bindings for REST and gRPC servers.

```yaml
server:
  http:
    port: 7700
    public: false
  grpc:
    port: 7701
```

---

# I18N / L10N Configuration

Localization and translation settings.

```yaml
i18n:
  enabled: true
  preferred_languages: ["fr", "en"]
  auto_translate: true
```

---

# Privacy Configuration

Global privacy rules.

```yaml
privacy:
  telemetry: false
  allow_external_pipelines: false
```

---

# Refresh Configuration

Controls system-level refresh logic, including auto-fetch of new content.

```yaml
refresh:
  enabled: true
  min_interval_seconds: 300
  default_interval_seconds: 86400
  rules:
    - match: "github.com/.+"
      interval_seconds: 43200
      fetch_new: false
    - match: "youtube.com/channel/.+"
      interval_seconds: 3600
      fetch_new: true
```

Notes:

- Plugins may programmatically adjust refresh rules for a given bookmark.
- `fetch_new` enables auto-discovery of new items for feed-like sources.
- Minimum interval prevents excessive refresh loops.

---

# Plugin Configuration

Plugins define their own configuration blocks via polymorphic keys.

```yaml
plugins:
  load:
    - name: notifications
      config:
        enabled: true
        channels:
          cli:
            - levels: ["info","warning"]
```

### Plugin Config Principles

- Plugins may define **any schema shape** within their `config` block.
- Plugin config merges following the same layer rules as core config.
- Plugins may add:
  - pipelines
  - bookmark types
  - refresh requirements
  - storage extensions (JSON only)
  - CLI commands
  - queue/storage backends
  - inter-plugin APIs

Plugins **never** require core modifications.

---

# Environment Variables

| Variable | Purpose |
|----------|----------|
| `CH_CONFIG` | Override config path |
| `CH_DATA_DIR` | Override storage directory |
| `CH_STORAGE_BACKEND` | Force backend type |
| `CH_REGISTRY_TOKEN_*` | Registry tokens |
| `CH_DISABLE_TELEMETRY` | Hard-disable telemetry |
| `CH_LANG` | Override preferred language list |

---

# Configuration Resolution Order

1. Defaults
2. System config
3. User config
4. Environment
5. Runtime flags

Plugins follow the same override logic.

---

# Validation Tools

```bash
ch config validate
ch config show
```

Validates:

- schema structure
- registry availability
- plugin load success
- pipeline discovery
- agent consistency
- refresh rules
- plugin-defined config integrity

---

# Summary

The ContextHelp configuration system is:

- **Layered** and predictable
- **Polymorphic** and plugin-extensible
- **Deterministic** and safe
- **Local-first** with privacy defaults
- **Declarative** for agents, pipelines, refresh, and plugins
- **Future-proof** with schema versioning

The addition of **refresh configuration**, **plugin-defined namespaces**, and **pipeline extensibility** ensures that plugins such as RSS Feed, Price Monitor, and Notifications can operate fully without touching the core engine.
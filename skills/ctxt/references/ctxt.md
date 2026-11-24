# `ctxt` — Actions & Intents

Find your objective below, then open the linked command reference. Global flags (`--format`, `--output`, `--profile`, `--instance`, `--config`, `--offline`, `--dry-run`, `--confirm`) are documented in [../SKILL.md](../SKILL.md).

> **Moved to `dpkms`**: `job`, `secret`, `key`, `detector`, and `dev` are deprecated shims in `ctxt` — use [dpkms.md](dpkms.md) instead.

## Capture & Ingest

| Objective | Command | When to pick this over siblings | Reference |
|---|---|---|---|
| Capture a URL, file, stdin, or clipboard | `ctxt capture` | Canonical capture verb; supports `--every` polling, `--inbox`, `--note` | [ctxt/capture.md](ctxt/capture.md) |
| Enqueue content with fine-grained enrichment controls | `ctxt analyze` | Default bare-`ctxt` verb; adds `--language`, `--translate`, `-f/--file` | [ctxt/analyze.md](ctxt/analyze.md) |
| Bulk-import from external apps/exports (19 sources) | `ctxt import` | One-shot/batch migration (browsers, PKM, chat, email, cloud, GitHub) | [ctxt/import/index.md](ctxt/import/index.md) |
| Pull structured records from external CLI adapters | `ctxt ingest` | Idempotent adapter pull (`cardamum`, `himalaya`, or stdin JSON) | [ctxt/ingest.md](ctxt/ingest.md) |
| Triage deferred or failed captures | `ctxt inbox` | `list`, `triage`, `discard`, `clear` | [ctxt/inbox.md](ctxt/inbox.md) |
| Subscribe to and sync RSS/Atom feeds | `ctxt feed` | `add`, `list`, `sync`, `delete` | [ctxt/feed.md](ctxt/feed.md) |
| Passively watch clipboard or local directories | `ctxt watch` | `start`, `stop`, `status`, `enable`, `disable` | [ctxt/watch.md](ctxt/watch.md) |

## Search, Browse & Inspect

| Objective | Command | When to pick this over siblings | Reference |
|---|---|---|---|
| Semantic, keyword, or hybrid natural-language search | `ctxt find` | Recall-oriented discovery; hybrid FTS+vector default, `--explain` | [ctxt/find.md](ctxt/find.md) |
| Deterministic filtered listing, facets, or RSQL query | `ctxt list` | Reproducible queries, `--q` RSQL, metadata facets, pagination, cursors | [ctxt/list.md](ctxt/list.md) |
| Write structured `--q` RSQL queries & filter facets | *(shared concept)* | Exact grammar, supported fields, SQLite vs Postgres caveats | [ctxt/query-language.md](ctxt/query-language.md) |
| Track incremental "since last read" positions | `ctxt cursor` | Named cursors used by `ctxt list --cursor <name> --advance` | [ctxt/cursor.md](ctxt/cursor.md) |
| Inspect a single knowledge object (or render via plugin) | `ctxt show` | Detail view by ID (or clipboard); `--raw` JSON or `--format <plugin>` | [ctxt/show.md](ctxt/show.md) |
| Create, list, traverse, or delete typed object links | `ctxt link` | Explicit graph edges between two object IDs | [ctxt/link.md](ctxt/link.md) |
| Query the knowledge changelog | `ctxt log` | Chronological history of object mutations (`--object`, `--actor`, `--type`) | [ctxt/log.md](ctxt/log.md) |
| View corpus & system counts at a glance | `ctxt stats` | Metrics dashboard (`--watch`); use `ctxt status` for daemon health | [ctxt/stats.md](ctxt/stats.md) |

## Synthesize, Export & Resurface

| Objective | Command | When to pick this over siblings | Reference |
|---|---|---|---|
| Generate a brief, plan, summary, or draft with citations | `ctxt compose` | LLM synthesis (`brief\|plan\|summary\|draft`), `--session`, `--export json` | [ctxt/compose.md](ctxt/compose.md) |
| Export an object using an output-generator plugin | `ctxt export` | Plugin export with `--dest` hint (vs `show --format`) | [ctxt/export.md](ctxt/export.md) |
| List, view, or regenerate compiled entity pages | `ctxt page` | Synthesized entity wiki pages (`list`, `show`, `refresh`) | [ctxt/page.md](ctxt/page.md) |
| Set, list, or clear scheduled reminders on objects | `ctxt remind` | Time-triggered resurfacing on specific object IDs | [ctxt/remind.md](ctxt/remind.md) |
| Surface high-relevance prior knowledge for a profile | `ctxt resurface` | Scored candidate queue (`refresh`, `dismiss`) | [ctxt/resurface.md](ctxt/resurface.md) |

## Curate & Maintain Graph

| Objective | Command | When to pick this over siblings | Reference |
|---|---|---|---|
| Edit an object's title, summary, tags, hints, or mentions | `ctxt edit` | Direct metadata overwrite by `<id>` | [ctxt/edit.md](ctxt/edit.md) |
| Permanently remove knowledge objects | `ctxt delete` | Irreversible; requires `--confirm=yes --confirm-token=<sha>` | [ctxt/delete.md](ctxt/delete.md) |
| Inspect/run c12n classification signals on text or an object | `ctxt classify` | Classification signal probe/persister | [ctxt/classify.md](ctxt/classify.md) |
| Re-run a specific AI enrichment step on an existing object | `ctxt reprocess` | Re-run `structured_metadata`, `entity_extractor`, or `tagger` | [ctxt/reprocess.md](ctxt/reprocess.md) |

## Organize, Scope & Federate

| Objective | Command | When to pick this over siblings | Reference |
|---|---|---|---|
| List, search, inspect entities and their backlinks | `ctxt entity` | Canonical `@namespace.slug` graph inspection | [ctxt/entity.md](ctxt/entity.md) |
| View or rebuild the topic index | `ctxt index` | Topic directory (`--refresh` rebuilds from scratch) | [ctxt/index.md](ctxt/index.md) |
| Manage focus profiles and per-profile schema vocabularies | `ctxt profile` | Profile CRUD, default lens, `profile schema` types/topics/rules/evolve | [ctxt/profile.md](ctxt/profile.md) |
| Connect, authenticate, sync, or submit to registries | `ctxt registry` | Federated namespaces, keychain auth, conflict reconciliation, metering | [ctxt/registry.md](ctxt/registry.md) |
| Register, list, migrate, or retire embedding models | `ctxt embeddings` | Embedding-model registry lifecycle | [ctxt/embeddings.md](ctxt/embeddings.md) |
| Run or evaluate the lateral discovery daemon | `ctxt lateral` | Opt-in lateral discovery daemon + offline fixture replay/metrics | [ctxt/lateral.md](ctxt/lateral.md) |
| Run automated health checks on the knowledge graph | `ctxt doctor` | Orphans, missing metadata, near-duplicates, stale objects | [ctxt/doctor.md](ctxt/doctor.md) |

## Instance, Config & System

| Objective | Command | When to pick this over siblings | Reference |
|---|---|---|---|
| Select or inspect the active `dpkms` instance | `ctxt instance` | `list`, `current`, `use <name\|-\|port>` | [ctxt/instance.md](ctxt/instance.md) |
| Check reachability and health of the active `dpkms` | `ctxt status` | Daemon health preflight (`--watch`, `--server`) | [ctxt/status.md](ctxt/status.md) |
| List or export SIEM-ready audit logs | `ctxt audit` | `list` and `export` (NDJSON, CEF, syslog) | [ctxt/audit.md](ctxt/audit.md) |
| Inspect, validate, lint, edit, backup, or restore config | `ctxt config` | `show`, `path`, `validate`, `doctor`, `edit`, `backup`, `restore` | [ctxt/config.md](ctxt/config.md) |
| Self-upgrade `ctxt` or orchestrate `dpkms` corpus upgrades | `ctxt upgrade` | Binary (`check/install/notes/snooze`) + corpus (`plan/run/status`) | [ctxt/upgrade.md](ctxt/upgrade.md) |
| Register `ctxt://` OS URL scheme or print packaging snippet | `ctxt uri` | `register`, `snippet` | [ctxt/uri.md](ctxt/uri.md) |
| Run onboarding wizard, interactive REPL, or TUI | `setup` / `shell` / `tui` | Use `ctxt shell` for low-latency multi-query sessions | [ctxt/interactive.md](ctxt/interactive.md) |
| Print version or generate shell completions | `ctxt version` / `ctxt completion` | `ctxt -v` / `ctxt version`; `ctxt completion [bash\|zsh\|fish\|powershell]` | *(inline)* |

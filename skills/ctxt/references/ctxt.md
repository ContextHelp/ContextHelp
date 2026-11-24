# `ctxt` — Actions & Intents

Find your objective below and open the linked command sheet (strictly 1 hop from here). Global flags (`--format`, `--output`, `--server`, `--profile`, `--instance`, `--as`, `--config`, `--offline`, `--dry-run`, `--confirm`) are documented in [../SKILL.md](../SKILL.md).

> Infrastructure commands (`detector`, `dev`, `key`, `secret`, `storage`) live under `dpkms` — see [dpkms.md](dpkms.md).

## Capture, Ingest & Jobs

| Objective | Command | When to pick this over siblings | Reference |
|---|---|---|---|
| Capture URL, file, stdin, clipboard, browser tabs/history, or ambient daemon — with optional expiry (`--ttl`/`--expires`) & encryption (`--encrypt`) | `ctxt capture` | Canonical capture verb; supports `browsers`, `tabs`, `history`, `schedule`, `--ambient`, `--inbox`, `--every`, expiry & encryption | [ctxt/capture.md](ctxt/capture.md) |
| Enqueue content with language/translation controls or structured `--data` JSON | `ctxt analyze` | Default bare-`ctxt` verb; adds `--data`, `--language`, `--translate`, `-f/--file` | [ctxt/analyze.md](ctxt/analyze.md) |
| Bulk-import generic `jsonl`/`csv`/`tsv`/Markdown files or directories | `ctxt import batch` | Generic structured batch import + `batch status` | [ctxt/import-batch.md](ctxt/import-batch.md) |
| Import browser & web bookmarks (`chrome`, `edge`, `firefox`, `safari`, `pinboard`, `raindrop`) | `ctxt import <src>` | HTML bookmark exports and bookmarking APIs | [ctxt/import-bookmarks.md](ctxt/import-bookmarks.md) |
| Import PKM & note vaults (`obsidian`, `logseq`, `notion`, `evernote`) | `ctxt import <src>` | Local vaults, Notion API, Evernote exports | [ctxt/import-notes-pkm.md](ctxt/import-notes-pkm.md) |
| Import email, chat & social archives (`email`, `slack`, `discord`, `twitter`, `linkedin`) | `ctxt import <src>` | IMAP/`.eml`/`.mbox` and platform exports | [ctxt/import-comms.md](ctxt/import-comms.md) |
| Import cloud drives & GitHub (`gdrive`, `dropbox`, `onedrive`, `github`) | `ctxt import <src>` | Cloud storage APIs and GitHub repos/star-lists | [ctxt/import-cloud-code.md](ctxt/import-cloud-code.md) |
| Pull structured records from external CLI adapters (`cardamum`, `himalaya`, stdin) | `ctxt ingest` | Idempotent live/recurring adapter sync (`--every`) | [ctxt/ingest.md](ctxt/ingest.md) |
| Triage deferred, pending, failed, or raw captures | `ctxt inbox` | `list`, `triage`, `discard`, `clear` | [ctxt/inbox.md](ctxt/inbox.md) |
| Subscribe to and sync RSS/Atom feeds | `ctxt feed` | `add`, `list`, `sync`, `delete` | [ctxt/feed.md](ctxt/feed.md) |
| Passively watch clipboard or directories (with optional `--ttl` & `--encrypt`) | `ctxt watch` | `start`, `stop`, `status`, `enable`, `disable` | [ctxt/watch.md](ctxt/watch.md) |
| Follow, retry, or cancel ingestion jobs | `ctxt job` | `list`, `status`, `retry`, `cancel` (for job logs, use `dpkms job log`) | [ctxt/job.md](ctxt/job.md) |

## Search, Browse & Inspect

| Objective | Command | When to pick this over siblings | Reference |
|---|---|---|---|
| Semantic, keyword, hybrid, or graph-visualized search | `ctxt find` | Recall-oriented discovery; hybrid FTS+vector default, `--explain`, `--graph` | [ctxt/find.md](ctxt/find.md) |
| Deterministic filtered listing, facets, change-feed, or RSQL query | `ctxt list` | Reproducible queries, `--q` RSQL, `--tagged` expressions, `--changed-after`, cursors | [ctxt/list.md](ctxt/list.md) |
| Write `--q` RSQL queries (`tag`, `mention`, `class`, `metadata.<k>`, `related`, `similar`) | *(shared concept)* | Exact RSQL grammar and supported fields | [ctxt/query-language.md](ctxt/query-language.md) |
| Track incremental "since last read" positions | `ctxt cursor` | Named cursors for `ctxt list --cursor <name> --advance` | [ctxt/cursor.md](ctxt/cursor.md) |
| Inspect a single knowledge object | `ctxt show` | Human detail or `--raw` JSON (by ID or clipboard) | [ctxt/show.md](ctxt/show.md) |
| Resolve an object ID or `@entity` ref to pipe-friendly body + provenance | `ctxt resolve` | One-shot agent/script resolver (`markdown` default or `--format json`) | [ctxt/resolve.md](ctxt/resolve.md) |
| Create, list, traverse, or delete typed object links | `ctxt link` | Explicit graph edges between two object IDs | [ctxt/link.md](ctxt/link.md) |
| Query the knowledge changelog | `ctxt log` | Chronological history of object mutations | [ctxt/log.md](ctxt/log.md) |
| View corpus & system counts at a glance | `ctxt stats` | Metrics overview (`--watch`); use `ctxt status` for daemon health | [ctxt/stats.md](ctxt/stats.md) |

## Synthesize, Export & Resurface

| Objective | Command | When to pick this over siblings | Reference |
|---|---|---|---|
| Generate a brief, plan, summary, or draft with citations | `ctxt compose` | LLM synthesis (`brief\|plan\|summary\|draft`), `--tagged`, `--mention`, `--export json` | [ctxt/compose.md](ctxt/compose.md) |
| Export a knowledge object as JSON | `ctxt export` | Direct JSON export of one object | [ctxt/export.md](ctxt/export.md) |
| List, view, or regenerate compiled entity wiki pages | `ctxt page` | `list`, `show`, `refresh` | [ctxt/page.md](ctxt/page.md) |
| Schedule, list, or clear reminders on objects | `ctxt remind` | Explicit time-triggered follow-ups | [ctxt/remind.md](ctxt/remind.md) |
| Surface high-relevance prior knowledge for the active profile | `ctxt resurface` | Scored candidate queue (`refresh`, `dismiss`) | [ctxt/resurface.md](ctxt/resurface.md) |

## Curate, Retention & Holds

| Objective | Command | When to pick this over siblings | Reference |
|---|---|---|---|
| Edit an object's title, summary, tags, mentions, or subtype | `ctxt edit` | Direct metadata overwrite by `<id>` | [ctxt/edit.md](ctxt/edit.md) |
| Permanently delete objects or erase by `source_key` with tombstone | `ctxt delete` | Irreversible; `--source-key --tombstone` blocks re-ingestion; needs `--confirm-token` | [ctxt/delete.md](ctxt/delete.md) |
| Schedule or clear automatic object expiration (whole object or NL scope) | `ctxt expire` | `set <id> <time-or-ttl> [--scope "..."]`, `clear <id>` | [ctxt/expire.md](ctxt/expire.md) |
| Place, list, or release legal/administrative holds preventing edits/deletion | `ctxt hold` | Transactional storage-layer hold guard (`list`, `place`, `release`) | [ctxt/hold.md](ctxt/hold.md) |
| Classify text or an object using `c12n` signals | `ctxt classify` | Classification signal probe/persister | [ctxt/classify.md](ctxt/classify.md) |
| Re-run an AI enrichment step on an existing object | `ctxt reprocess` | `structured_metadata`, `entity_extractor`, or `tagger` | [ctxt/reprocess.md](ctxt/reprocess.md) |

## Organize, Scope & Federate

| Objective | Command | When to pick this over siblings | Reference |
|---|---|---|---|
| List, search, inspect entities and their backlinks | `ctxt entity` | `list`, `show`, `search`, `backlinks` | [ctxt/entity.md](ctxt/entity.md) |
| View or rebuild the topic index | `ctxt index` | Topic directory (`--refresh`) | [ctxt/index.md](ctxt/index.md) |
| Manage focus profiles and per-profile schema vocabularies | `ctxt profile` | `list`, `view`, `create`, `rm`, `set`, `unset`, `schema {...}` | [ctxt/profile.md](ctxt/profile.md) |
| Connect, auth, sync, approve queued entities, or submit to registries | `ctxt registry` | 15 subcmds incl. `pending`, `approve`, `reject`, `login --origin`, `sync --reconcile` | [ctxt/registry.md](ctxt/registry.md) |
| Register, migrate, promote, deprecate, or purge embedding models | `ctxt embeddings` | Full lifecycle (`list`, `provider`, `register`, `migrate`, `set-default`, `deprecate`, `purge`) | [ctxt/embeddings.md](ctxt/embeddings.md) |
| Run or evaluate the lateral discovery daemon | `ctxt lateral` | `start`, `status`, `config show`, `eval replay/metrics` | [ctxt/lateral.md](ctxt/lateral.md) |
| Run automated health checks on the knowledge graph | `ctxt doctor` | Orphans, missing metadata, near-duplicates, stale objects | [ctxt/doctor.md](ctxt/doctor.md) |

## Access, Sharing & Governance

| Objective | Command | When to pick this over siblings | Reference |
|---|---|---|---|
| Inspect your identity and manage your own API keys, identities, and browser sessions | `ctxt auth` | `whoami`, `key`, `identity`, `session` | [ctxt/auth.md](ctxt/auth.md) |
| Administer instance principals, roles, sign-in links, and identities | `ctxt principal` | Admin-only: `list`, `show`, `create`, `suspend`, `revoke`, `link`, `identity` | [ctxt/principal.md](ctxt/principal.md) |
| Share knowledge via audiences, policies, grants, ceilings, invites, previews & read audit | `ctxt share` | `audience`, `policy`, `grant`, `revoke`, `ceiling`, `invite`, `preview`, `audit`, `template` | [ctxt/share.md](ctxt/share.md) |
| Request, list, inspect, or revoke time-bounded steward access | `ctxt steward` | Elevated temporary access (`request`, `list`, `show`, `revoke`) | [ctxt/steward.md](ctxt/steward.md) |
| Review two-person approval requests (approve, reject, withdraw) | `ctxt approval` | `list`, `show`, `approve`, `reject`, `withdraw` | [ctxt/approval.md](ctxt/approval.md) |

## Instance, Config & System

| Objective | Command | When to pick this over siblings | Reference |
|---|---|---|---|
| Select or inspect the target `dpkms` instance | `ctxt instance` | `list`, `current`, `use <name\|-\|port>` | [ctxt/instance.md](ctxt/instance.md) |
| Check reachability and health of the active `dpkms` | `ctxt status` | Quick daemon health preflight | [ctxt/status.md](ctxt/status.md) |
| List or export SIEM-ready audit logs | `ctxt audit` | `list`, `export` (NDJSON, CEF, syslog) | [ctxt/audit.md](ctxt/audit.md) |
| Inspect, resolve paths, validate, lint, edit, backup, or restore config | `ctxt config` | `show`, `path`, `paths`, `validate`, `doctor`, `edit`, `backup`, `restore` | [ctxt/config.md](ctxt/config.md) |
| Self-upgrade `ctxt` or orchestrate `dpkms` corpus upgrades | `ctxt upgrade` | `check/install/notes/snooze` + `plan/run/status` | [ctxt/upgrade.md](ctxt/upgrade.md) |
| Register `ctxt://` OS URL scheme or print packaging snippet | `ctxt uri` | `register`, `snippet` | [ctxt/uri.md](ctxt/uri.md) |
| Onboarding wizard, interactive REPL, TUI, or web UI sign-in | `setup` / `shell` / `tui` / `ui` | `ctxt shell` cuts startup latency; `ctxt ui open --no-browser` prints link | [ctxt/interactive.md](ctxt/interactive.md) |
| Print version or generate shell completions | `ctxt version` / `ctxt completion` | `ctxt -v` / `ctxt version`; `ctxt completion [bash\|zsh\|fish\|powershell]` | *(inline)* |

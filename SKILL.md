---
name: ctxt
description: |
  Work with the ctxt CLI — an agentic context brain for capture, search, and composition
  on top of dPKMS (decentralized personal knowledge substrate).

  Automatically applies when: task involves capturing content, searching knowledge,
  composing briefs/plans, or managing focus profiles via the ctxt CLI.

  Use when: capturing content to knowledge base, running semantic search, generating
  compositions, managing config/profiles, or operating the dPKMS background worker.

  Description checklist:
  - [x] What it does: context-as-a-service CLI for humans and AI agents
  - [x] When to auto-activate: any ctxt/dpkms CLI operation
  - [x] Key surfaces: capture, search, compose, config, registry, entity
  - [x] Local-first, offline-capable, scriptable
---

# ctxt CLI Skill

Teach an LLM agent to operate `ctxt` and `dpkms` effectively.
Follow exactly; do not improvise flags or subcommand names.

---

## Level 1 — Essentials (start here)

### What is ctxt?

Two binaries, one system:

| Binary | Role | Key concern |
|--------|------|-------------|
| `ctxt` | User-facing brain | capture, search, compose, profiles |
| `dpkms` | Infrastructure substrate | server, jobs, storage, graph |

All ingestion is async: `ctxt` enqueues a job → `dpkms serve` runs the pipeline →
knowledge object is stored → available for search/compose.

### Install

```bash
brew install contexthelp
# or
curl -sL https://context.help/install.sh | bash
```

### Start the worker (required before any pipeline runs)

```bash
dpkms serve
```

### Capture content (most common operation)

```bash
# Bare invocation → reads clipboard
ctxt

# Inline text
ctxt "The API rate limit is 1000 req/min" --hints "#api #limits"

# Piped stdin
echo "Fix signup flow" | ctxt --mentions "@product.auth"

# From file
ctxt analyze --file notes.md --type text

# URL (auto-detected)
ctxt "https://example.com/blog/post"
```

### Search

```bash
# Semantic (NLQ)
ctxt find "rate limiting strategies"

# No query → clipboard
ctxt find

# With profile lens
ctxt find "auth issues" --profile engineer
```

### Compose

```bash
ctxt make brief --tag api --since 2026-01-01
ctxt make plan --mention "@product.auth"
ctxt make summary --profile founder
ctxt make draft --output report.md
```

---

## Level 2 — Common Workflows

### Workflow: capture → wait → inspect

```bash
# Capture and block until processed
ctxt "content here" --wait
# Returns knowledge object ID

# Open the result
ctxt open <id>

# Or list recent
ctxt list --limit 10 --sort recent
```

### Workflow: triage inbox

```bash
# Capture to inbox (deferred processing)
ctxt analyze "rough idea" --inbox

# See what's pending
ctxt inbox list

# Promote to pipeline
ctxt inbox triage <id> --pipeline text.long

# Discard noise
ctxt inbox discard <id>
```

### Workflow: query by filter

```bash
# By tag
ctxt list --tag api,security

# By mention
ctxt list --mention "@product.auth"

# By type + date window
ctxt list --type url --after 2026-01-01 --before 2026-03-01

# AST query
ctxt list --q "tag:api AND mention:@stripe.api"

# JSON output for scripting
ctxt list --json | jq '.[] | .id'
```

### Workflow: entity graph exploration

```bash
ctxt entity list
ctxt entity show stripe.api
ctxt entity search "payment"
ctxt entity backlink stripe.api    # what references this entity?
```

### Workflow: registry management

```bash
ctxt registry list
ctxt registry add core https://registries.context.help/core
ctxt registry sync core
ctxt registry info core
```

---

## Level 3 — Reference

### Full command map

| Command | Purpose |
|---------|---------|
| `ctxt [content]` | Capture (default; clipboard fallback) |
| `ctxt analyze` | Explicit capture alias |
| `ctxt inbox list/triage/discard/clear` | Inbox triage |
| `ctxt job list/status/log/retry/cancel` | Job queue ops |
| `ctxt list` | Query knowledge objects |
| `ctxt find <query>` | Semantic search |
| `ctxt open <id>` | Show object detail |
| `ctxt edit --id <id>` | Edit object metadata |
| `ctxt delete` | Remove objects |
| `ctxt make <type>` | Generate compositions |
| `ctxt profile list/show/create/delete/set-default` | Focus profiles |
| `ctxt config show/path/validate/edit` | Config ops |
| `ctxt registry list/add/remove/info/sync` | Registry ops |
| `ctxt entity list/show/search/backlink` | Entity ops |
| `ctxt secret get/set/list` | Secrets backend |
| `ctxt uri register` | Register ctxt:// OS handler |
| `dpkms serve` | Start worker + REST/gRPC |
| `dpkms housekeeping vacuum/reindex/compact/prune` | DB maintenance |

### analyze flags

| Flag | Description |
|------|-------------|
| `--type <text\|url\|image\|audio\|video\|feed\|auto>` | Input type override |
| `--hints "<#h1 #h2>"` | Influence tagging |
| `--mentions "<@slug1 @slug2>"` | Attach explicit mentions |
| `--file <path>` | Read from file |
| `--profile <name>` | Focus profile |
| `--pipeline <name>` | Force pipeline |
| `--raw` | Disable AI; store as-is |
| `--wait` | Block until job completes |
| `--inbox` | Defer to inbox |
| `--output <json\|yaml\|id>` | Output format |

### list/find flags

| Flag | Description |
|------|-------------|
| `--type <type>` | Filter by object type |
| `--tag <t1,t2>` | Filter by tags |
| `--hint <#h>` | Filter by hints |
| `--mention <@slug>` | Filter by mention |
| `--after / --before <ISO>` | Date window |
| `--profile <name>` | Profile lens |
| `--q "<query>"` | AST query |
| `--limit <n>` | Max results |
| `--sort <recent\|match\|score\|weight\|view>` | Sort order |
| `--json / --yaml` | Machine output |

### make types

| Type | Output |
|------|--------|
| `brief` | Executive brief |
| `plan` | Action plan |
| `summary` | Summary |
| `draft` | Publish-ready draft |

### Exit codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | General error |
| 2 | Invalid flags/input |
| 3 | Storage error |
| 4 | Registry error |
| 5 | Pipeline failure |
| 6 | Agent config error |
| 7 | Job execution error |

### Environment variables

| Variable | Purpose |
|----------|---------|
| `CTXT_CONFIG` | Config path override |
| `CTXT_DATA_DIR` | Data directory override |
| `CTXT_PROFILE` | Default focus profile |
| `DPKMS_DATA_DIR` | dPKMS data directory |
| `DPKMS_WORKERS` | Worker thread count |

---

## Conventions

### Mention syntax

Entities are referenced with `@namespace.slug`:

```
@product.auth     # entity "auth" in namespace "product"
@stripe.api       # entity "api" in namespace "stripe"
```

Use in `--mentions` flags and `--q` AST queries.

### Hint syntax

Hints are `#tag`-prefixed freeform influence signals (not formal tags):

```
--hints "#ux #mobile #p1"
```

### Clipboard fallback

`ctxt`, `ctxt analyze`, `ctxt find`, and `ctxt open` all fall back to the
system clipboard when no content/query/ID argument is given.

### Two-phase ingestion

`ctxt analyze` returns a **Job ID** immediately.
Use `--wait` to block and get the **Knowledge Object ID**.
Or poll: `ctxt job status <job-id>`.

### Config validation

Before running in CI or agent context:

```bash
ctxt config validate --check-secrets
```

---

## Glossary

| Term | Meaning |
|------|---------|
| **Knowledge Object** | Core data unit; produced from any input by a pipeline |
| **Mention** | `@namespace.slug` reference to a canonical entity |
| **Entity** | Canonical concept with stable ID, aliases, relationships |
| **Focus Profile** | Role/project lens affecting ranking, pipelines, surfacing |
| **Pipeline** | Named sequence of steps transforming input → knowledge object |
| **Registry** | Decentralized source of entity definitions / knowledge packs |
| **Job** | Durable unit of async background work |
| **Inbox** | Staging area for deferred-triage captures |
| **Hint** | `#tag`-shaped signal influencing AI enrichment |

---

## Agent Patterns

### Pattern: capture + extract ID for downstream use

```bash
id=$(ctxt "content" --wait --output id)
ctxt open "$id" --json | jq '.mentions'
```

### Pattern: search → compose

```bash
# Find relevant objects, compose brief
ctxt find "auth rate limiting" --limit 20 --json > /tmp/results.json
ctxt make brief --tag auth --mention "@product.auth" --output /tmp/brief.md
```

### Pattern: scripted batch capture

```bash
while IFS= read -r line; do
  ctxt "$line" --hints "#batch #import" --raw
done < items.txt
```

### Pattern: agent worldview via profile

```bash
# All operations scoped to a profile
export CTXT_PROFILE=research
ctxt find "LLM memory approaches"
ctxt make summary --since 2026-02-01
```

---

## Anti-Patterns

- Never skip `dpkms serve` — pipelines do not run without the worker.
- Never use `--raw` when enrichment is needed; it bypasses AI entirely.
- Never assume synchronous processing; use `--wait` or `ctxt job status`.
- Never pass secrets on CLI (`ctxt secret set KEY val` exposes value in shell history).
  Use the backend's native tooling instead.
- Never filter with both `--tag` and `--q` containing tag filters simultaneously
  (they compound; can produce unexpectedly narrow results).

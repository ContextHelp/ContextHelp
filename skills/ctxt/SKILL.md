---
name: ctxt
description: |
  Operate the ContextHelp CLI pair — ctxt (agentic knowledge brain) and dpkms
  (decentralized personal knowledge substrate).

  Use when: capturing (with optional TTL/expiry or encryption), importing,
  searching, filtering, resolving, composing, curating, placing holds, sharing,
  or governing knowledge with ctxt; operating the dpkms daemon, storage
  encryption, job queue, pipelines, detectors, backups, access posture, or
  secrets; or running an agent memory and retrieval loop with provenance.
---

# ContextHelp (`ctxt` + `dpkms`)

Two binaries, one local-first system:

| Binary | Role | Start here by intent |
|---|---|---|
| `ctxt` | **Knowledge brain** — capture (expiry + encryption + browser/ambient), import, search, list, resolve, compose, curate, holds, organize, share, govern | [references/ctxt.md](references/ctxt.md) |
| `dpkms` | **Infrastructure substrate** — daemon lifecycle (local/pod), storage encryption/rekey, backups, housekeeping, jobs, pipelines, detectors, access posture, secrets, keys | [references/dpkms.md](references/dpkms.md) |

For multi-step agent memory and deterministic-first retrieval with provenance:
- [references/workflows/agent-memory.md](references/workflows/agent-memory.md) (taxonomy & presets: [references/workflows/memory-taxonomy.md](references/workflows/memory-taxonomy.md))

## How to navigate this skill

1. Pick `ctxt` or `dpkms` above by objective (max 2 hops to any command sheet).
2. Open its intent router (`references/ctxt.md` or `references/dpkms.md`) to select the exact command.
3. Open only the command sheet you need (`references/{ctxt,dpkms}/<sheet>.md`).

## Shared invariants (read once)

- **Async ingestion**: capture/analyze/import enqueue jobs; `dpkms serve` executes pipelines. Check readiness via `ctxt status` or `dpkms healthcheck`. Pass `--wait` to block, or track jobs via `ctxt job status <id>` / `dpkms job status <id>`.
- **Default bare `ctxt`**: `ctxt "text"` defaults to `analyze`; `ctxt ctxt://<id>` or `ctxt ctxt://search/<q>` dispatches a URI; empty input falls back to clipboard (`CTXT_NO_CLIPBOARD=1` disables).
- **Moved to `dpkms`**: `detector`, `dev`, `key`, and `secret` live exclusively under `dpkms` (removed from `ctxt`). `job` is available on both `ctxt` (client view) and `dpkms` (includes `job log`).
- **Syntax conventions**:
  - Mentions: `@namespace.slug` (or `ctxt://` URI on `list`/`compose`); repeatable, comma- or space-separated on `capture`/`analyze`/`edit`.
  - Hints: `--hint` (repeatable, comma- or space-separated, leading `#` optional).
  - Tags: `--tagged` on `list`/`compose` supports `+required,boost,-excluded` expressions; `--tagged` on `delete` takes one literal tag.
  - Expiry & encryption: `capture` and `watch start` support opt-in `--ttl`/`--expires`/`--expires-scope` and `--encrypt`/`--encrypt-scope` (natural-language scopes like `"PII and phone numbers"` or `"raw content, keep summary"`).
- **Destructive confirmation**: destructive commands require `--confirm=yes --confirm-token=<sha>` (run once or with `--dry-run` to preview and get the token).
- **Global flags** (inherited by every subcommand; omitted from command sheets):

| Flag | Scope | Purpose |
|---|---|---|
| `--format <text\|json\|yaml>` (`-f`) | both | Machine-readable output (strictly validated; prefer `json` for agents) |
| `--output <path>` (`-o`) | both | Write output to file |
| `--config <path\|k=v>` (`-c`) | both | Config file or `key=value` override (repeatable) |
| `--instance <name\|port>` | both | Target `dpkms` instance (env: `CTXT_INSTANCE`) |
| `--offline` | both | Disable network calls; local-only mode |
| `--quiet` (`-q`) / `--verbose` (`-v`) | both | Suppress or increase logs (`-v` stackable) |
| `--dry-run` | write/destructive | Preview without mutating (where supported) |
| `--confirm` / `--confirm-token` | destructive | Non-interactive confirmation (`auto\|yes\|no\|prompt`) |
| `--idempotency-key <key>` | conditional writes | Replay-safe dedup key |
| `--server <url>` / `--profile <name>` / `--as <viewer>` | `ctxt` | Target server URL / focus profile (`CTXT_PROFILE`) / owner read-as preview |
| `--data-dir <path>` / `--server-url <url>` | `dpkms` | Storage dir (`CTXT_DATA_DIR`) / daemon API URL |

## Quick sanity check

```bash
dpkms healthcheck                                     # verify substrate is up
ctxt capture "Decision: use RSQL" --ttl 30d --wait    # capture with expiry
ctxt find "RSQL decision" --format json               # hybrid FTS+vector search
```

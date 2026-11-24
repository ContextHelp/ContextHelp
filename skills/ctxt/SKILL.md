---
name: ctxt
description: |
  Operate the ContextHelp CLI pair — ctxt (agentic knowledge brain) and dpkms
  (decentralized personal knowledge substrate).

  Use when: capturing, importing, searching, filtering, composing, curating, or
  organizing knowledge with ctxt; operating the dpkms daemon, job queue,
  pipelines, detectors, backups, housekeeping, or secrets; or running an agent
  memory and retrieval loop with provenance.
---

# ContextHelp (`ctxt` + `dpkms`)

Two binaries, one local-first system:

| Binary | Role | Start here by intent |
|---|---|---|
| `ctxt` | **Knowledge brain** — capture, import, search, list, compose, curate, organize, profile, federate | [references/ctxt.md](references/ctxt.md) |
| `dpkms` | **Infrastructure substrate** — daemon lifecycle, jobs, pipelines, detectors, backups, housekeeping, secrets, keys, plugin dev | [references/dpkms.md](references/dpkms.md) |

For multi-step agent memory and deterministic-first retrieval with provenance:
- [references/workflows/agent-memory.md](references/workflows/agent-memory.md) (taxonomy & presets: [references/workflows/memory-taxonomy.md](references/workflows/memory-taxonomy.md))

## How to navigate this skill

1. Identify your **objective** and pick `ctxt` or `dpkms` above.
2. Open its intent router (`references/ctxt.md` or `references/dpkms.md`) to choose the canonical command.
3. Open only the command reference you need (`references/{ctxt,dpkms}/<command>.md`).

## Shared invariants (read once)

- **Async ingestion**: capture/analyze/import enqueue jobs; `dpkms serve` executes pipelines. Check readiness with `ctxt status` or `dpkms healthcheck`. Pass `--wait` to block for an object ID, or track jobs via `dpkms job status <id>`.
- **Default bare `ctxt`**: `ctxt "text"` defaults to `analyze`; `ctxt ctxt://<id>` or `ctxt ctxt://search/<q>` dispatches a URI; empty input falls back to clipboard (disable with `CTXT_NO_CLIPBOARD=1`).
- **Moved commands**: `job`, `secret`, `key`, `detector`, and `dev` live under **`dpkms`**, not `ctxt` (legacy `ctxt` shims are deprecated).
- **Syntax conventions**:
  - Mentions: `@namespace.slug` (e.g. `@project.checkout`, `@stripe.api`).
  - Hints: `--hint` (repeatable or CSV, e.g. `--hint "#ux,#mobile"` or `--hint research`).
  - Tags filter: `--tagged` (comma-separated, e.g. `--tagged ux,onboarding`).
- **Destructive confirmation**: destructive `ctxt` commands require kit's two-step confirmation (`--confirm=yes --confirm-token=<sha>`); run once (or with `--dry-run`) to inspect scope and obtain the token.
- **Global flags** (inherited by every subcommand; not repeated in per-command sheets):

| Flag | Scope | Purpose |
|---|---|---|
| `--format <text\|json\|yaml>` (`-f`) | both | Machine-readable output (prefer `json` in agent scripts) |
| `--output <path>` (`-o`) | both | Write output to file |
| `--config <path\|k=v>` (`-c`) | both | Config file or key=value override (repeatable) |
| `--instance <name\|port>` | both | Target `dpkms` instance (env: `CTXT_INSTANCE`) |
| `--offline` | both | Disable network calls; local-only mode |
| `--quiet` (`-q`) / `--verbose` (`-v`) | both | Suppress or increase logs (`-v` is stackable) |
| `--dry-run` | write/destructive | Preview without mutating (where supported) |
| `--confirm` / `--confirm-token` | destructive | Non-interactive confirmation (`auto\|yes\|no\|prompt`) |
| `--idempotency-key <key>` | conditional writes | Replay-safe deduplication key |
| `--profile <name>` | `ctxt` | Focus profile lens (env: `CTXT_PROFILE`) |
| `--data-dir <path>` / `--server-url <url>` | `dpkms` | Storage path (`DPKMS_DATA_DIR`) / API URL (`http://localhost:8080`) |

## Key environment variables

| Variable | Effect |
|---|---|
| `CTXT_CONFIG` | Explicit config path override |
| `CTXT_DATA_DIR` / `DPKMS_DATA_DIR` | Storage directory override |
| `CTXT_PROFILE` | Default focus profile |
| `CTXT_INSTANCE` | Target `dpkms` instance |
| `CTXT_NO_CLIPBOARD=1` | Disable clipboard fallback and clipboard watcher |
| `DPKMS_WORKERS` | Worker thread count for `dpkms serve` |

## Quick sanity check

```bash
dpkms healthcheck                           # verify substrate is up
ctxt capture "Decision: use RSQL" --wait    # capture and block for object ID
ctxt find "RSQL decision" --format json     # hybrid FTS+vector search
```

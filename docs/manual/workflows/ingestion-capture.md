# Workflow: Ingestion and Capture

## Goal

Capture raw content quickly and reliably so it becomes searchable and composable context.

## Scope

- Text, URL, image, audio, video, documents, feed sync, batch import
- Source importers (browser bookmarks, cloud drives, chat exports, note vaults, social archives)

## Primary stories

- `US-0001` to `US-0008`
- `US-0300` to `US-0317` (importer interface + source-specific importers)
- `US-0106` (enqueue via dPKMS API)
- `US-0113` (ctxt analyze API client)

## Prerequisites

1. `dpkms` server is running and reachable from this machine:

```bash
dpkms serve
```

2. `ctxt` client is configured (`~/.config/contexthelp/ctxt.yaml`) with that instance's URL and a token that may write (`writer` or `admin`). `ctxt analyze`, bare `ctxt` fed on stdin, `ctxt capture` and the browser captures send to the one instance ctxt resolves (`--server`, `--instance`, `ctxt instance use`, else the first `server.urls` entry). Nothing is queued on this machine.
3. You know the content type you are ingesting (`text`, `url`, `image`, `audio`, `video`, `feed`, or `auto`).

## Procedure

### Step 1: Ingest content

Text:

```bash
echo "Checkout conversion dropped after copy change" | ctxt analyze --type text --hints "#checkout #conversion"
```

URL:

```bash
ctxt analyze https://example.com/postmortem --type url
```

Local file:

```bash
ctxt analyze --file ./notes/meeting.md --type text
```

Image:

```bash
ctxt analyze --file ./assets/wireframe.png --type image
```

Agent/integration path via dPKMS pipeline command:

```bash
dpkms pipeline enqueue --type text --pipeline text.short "Quick ingestion payload"
```

Importer path (current runtime + story target):

```bash
# Implemented runtime path (Chrome bookmarks)
ctxt import chrome --file ./bookmarks.html --dry-run
ctxt import chrome --file ./bookmarks.html

# Story-target paths defined by US-0305+ (when implemented)
ctxt import gdrive --since 2026-01-01
ctxt import notion --max-items 500
ctxt import linkedin --file ./linkedin-export.zip
```

### Step 2: Apply context controls when needed

Use focus profile:

```bash
ctxt analyze "Pricing experiment notes" --type text --profile growth
```

Force specific pipeline:

```bash
ctxt analyze --file ./docs/architecture.md --type text --pipeline text.long
```

Attach explicit mentions:

```bash
ctxt analyze "Action for checkout redesign" --type text --mention "@project.checkout-redesign"
```

### Step 3: Track job execution

Jobs run on the dpkms instance, local or remote. `ctxt job` reads them over its API with the token `ctxt` already uses, so you don't need a shell on the dpkms host.

List recent jobs:

```bash
ctxt job list --limit 20
```

Filter by state:

```bash
ctxt job list --state pending --limit 20
ctxt job list --state failed --limit 20
```

Inspect one job:

```bash
ctxt job status <job_id>
```

Retry failures:

```bash
ctxt job retry <job_id>
```

Cancel a pending or running job. Cancel needs an `admin` token, and the first run without `--confirm-token` prints the token to pass back:

```bash
ctxt job cancel <job_id> --confirm-token=<token>
```

### Step 4: Verify resulting objects

List objects by type/tag/date:

```bash
ctxt list --type url --limit 20
ctxt list --tagged checkout --after 2026-02-01
```

Open a specific object:

```bash
ctxt open <object_id> --output json
```

## Batch ingestion patterns

Batch URL ingestion from a file:

```bash
xargs -I{} ctxt analyze "{}" --type url < urls.txt
```

Batch Markdown ingestion from a directory:

```bash
find ./imports -type f -name "*.md" -print0 | xargs -0 -I{} ctxt analyze --file "{}" --type text
```

## Import starred repos and GitHub star lists

```bash
ctxt import github --star-lists --account stars --dry-run   # plan only
ctxt import github --star-lists --account stars             # submit
```

`--account` names a `github.accounts[]` entry ([config](../reference/config-and-permissions.md#call-the-github-api)); its token must belong to the user whose stars are read, because GitHub Lists are visible only to their owner. No `gh` CLI, no `GITHUB_TOKEN` fallback when an account is named. Starred repos and the Lists are read through GraphQL.

Each repo becomes a URL capture, so `url.github.repo` runs and the repo dedups to one object (`forge:github:<owner/repo>`). The import adds:

| Field | Value |
|-------|-------|
| source key | `github:<owner>/<repo>` (profile-scoped: the active `--profile`) |
| hints (tags) | `starred`, and `star-list:<slug>` per List holding the repo |
| `metadata.data.account` | login of the token owner |
| `metadata.data.starred` | `true` when starred (a List can hold an unstarred repo) |
| `metadata.data.star_lists` | `[{slug, name, description}]`, the current List membership and vocabulary |

GitHub stays the source of truth: the vocabulary is rewritten from GitHub on every changed submission. Hints are only ever added to a stored object, so a repo that left a List keeps its `star-list:<slug>` tag; read `metadata.data.star_lists` for current membership.

Reruns are incremental: a state file under `$XDG_STATE_HOME/ctxt/import/` (per instance, profile and account) keeps what was submitted, and only repos whose hints or vocabulary changed are submitted again. `--full` ignores it; losing it costs one idempotent full pass. `--dry-run` submits nothing and writes no state. The report counts `Submitted`, `Unchanged` and `Failed` (`--format json` for the per-repo rows); a failed repo is retried on the next run and the command exits non-zero. Repos unstarred since the last run are not removed.

## Outputs to validate

- Object created with stable ID
- Source metadata and timestamps captured
- Enrichment kickoff successfully queued
- Failed jobs are visible and retryable
- Importer runs report scanned/imported/skipped/failed counts

## Common failure modes

### `PREREQUISITE: dpkms at <url> unreachable` (exit 70)

- Nothing answered at the resolved instance, and nothing was queued: run the command again once it answers.
- Start `dpkms serve` there, or select another instance with `--instance <name>`.
- ctxt never tries the next `server.urls` entry.

### `UNAUTHORIZED: dpkms at <url> returned 401` or `403` (exit 5)

- The token is missing, wrong, or a `reader` token without `write:objects`.
- Set `server.token` (or the entry's `token`) to a `writer` or `admin` token. The request is not retried.

### Jobs remain pending too long

- Confirm `dpkms serve` is running.
- Check worker load via `ctxt job list --state running`.
- Reduce ingest volume and retry failed jobs.

### Wrong pipeline/type detected

- Set `--type` explicitly.
- Force `--pipeline` for known document classes.

### Captured object missing expected context

- Re-ingest with `--hints`, `--mentions`, and `--profile`.
- Validate with `ctxt open <object_id> --output json`.

## Related references

- [`../reference/api-cli-reference.md`](../reference/api-cli-reference.md)
- [`../operations/runbook.md`](../operations/runbook.md)
- [`../troubleshooting/faq.md`](../troubleshooting/faq.md)
- [`docs/stories/ingestion/US-0300-importer-extension-interface.md`](https://github.com/ContextHelp/ContextHelp/blob/next/docs/stories/ingestion/US-0300-importer-extension-interface.md)

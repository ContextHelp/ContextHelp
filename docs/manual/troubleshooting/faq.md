# FAQ and Diagnostic Playbooks

## 1) Nothing works: where do I start?

Run baseline checks first:

```bash
ctxt version
dpkms version
ctxt config validate
```

Start server in a dedicated terminal:

```bash
dpkms serve
```

Verify health endpoint:

```bash
curl http://127.0.0.1:8080/health
```

If these fail, fix runtime/config before deeper troubleshooting.

## 2) Why are my ingestion jobs stuck in `pending`?

Check queue states:

```bash
ctxt job list --state pending --limit 50
ctxt job list --state running --limit 50
```

Inspect a blocked job:

```bash
ctxt job status <job_id>
ctxt job log <job_id>
```

Typical fixes:

1. Ensure `dpkms serve` is running.
2. Increase workers (`dpkms serve --workers 8`) for heavy backlogs.
3. Retry failed jobs:

```bash
ctxt job retry <job_id>
```

Related stories: `US-0008`, `US-0015`, `US-0032`, `US-0033`, `US-0036`, `US-0301` to `US-0317`

## 3) Why did a job fail repeatedly?

Capture detailed failure info:

```bash
ctxt job status <job_id>
ctxt job log <job_id>
```

Then verify the input class and pipeline:

1. Re-run with explicit `--type`.
2. Re-run with explicit `--pipeline` for known content class.
3. Add structural cues (`--hints`, `--mentions`, `--profile`).

Example retry with tighter controls:

```bash
ctxt analyze --file ./notes/retry-case.md --type text --pipeline text.long --profile engineering
```

Related stories: `US-0015`, `US-0033`, `US-0113`

## 4) Why are search results irrelevant?

Start broad, then constrain:

```bash
ctxt find "checkout conversion issues" --limit 10
ctxt list --tag checkout,conversion --after 2026-02-01 --limit 20
ctxt list --q "type==url;tag=in=(checkout,conversion)" --limit 20
```

Inspect top results directly:

```bash
ctxt open <object_id>
ctxt open <object_id> --output json
```

Typical fixes:

1. Apply correct `--profile`.
2. Add date/type/tag filters.
3. Shift from semantic (`find`) to structured (`list --q`) for deterministic retrieval.

Related stories: `US-0016`, `US-0017`, `US-0018`, `US-0021`, `US-0051`

## 5) Why is search slow?

Check whether slowness is local indexing or federation:

1. Run local-only style filtered queries (`ctxt list ...`) and compare latency.
2. Check registry connectivity/sync state:

```bash
ctxt registry list
ctxt registry info <registry-name>
ctxt registry sync <registry-name>
```

3. Rebuild indexes during maintenance windows:

```bash
dpkms housekeeping reindex
```

Related stories: `US-0019`, `US-0032`, `US-0110`, `US-0111`

## 6) Why is generated composition low quality?

Validate source scope first:

```bash
ctxt find "checkout redesign decisions" --limit 10
ctxt list --tag checkout,decision --after 2026-02-01 --limit 20
```

Then regenerate with tighter filters:

```bash
ctxt make brief --tag checkout,decision --since 2026-02-01 --output-file checkout-brief.md
ctxt make plan --mention @project.checkout-redesign --since 2026-02-01 --output-file checkout-plan.md
```

If still weak, inspect source objects and verify enrichment completeness.

Related stories: `US-0022`, `US-0023`, `US-0025`, `US-0056` to `US-0060`

## 7) Why do pipeline or step operations fail?

Check pipeline/step inventory and registry definitions:

```bash
dpkms pipeline list --include-archived
dpkms pipeline show <pipeline-name>
dpkms pipeline step list
dpkms pipeline step registry list
```

If a step install fails:

```bash
dpkms pipeline step install <step-name> --registry <registry-url>
```

Validate registry and manifest availability:

```bash
dpkms pipeline step registry update <registry-url>
```

Related stories: `US-0101` to `US-0113`, `US-0029`

## 8) Why do plugin-style integrations fail at runtime?

Treat as compatibility + capability issue:

1. Confirm integration is tested against current runtime version.
2. Re-check config and isolation assumptions.
3. Verify pipeline sandbox/registry settings in admin docs.

Helpful references:

- [`../reference/config-and-permissions.md`](../reference/config-and-permissions.md)
- [`../admin-extensibility/plugin-development.md`](../admin-extensibility/plugin-development.md)

Related stories: `US-0042` to `US-0045`, `US-0112`

## 9) How do I safely run maintenance commands?

Use controlled order:

```bash
dpkms housekeeping vacuum
dpkms housekeeping reindex
dpkms housekeeping compact
```

Optional retention prune:

```bash
dpkms housekeeping prune --before 2025-01-01
```

Guidelines:

1. Run during low-traffic windows.
2. Verify query behavior after reindex.
3. Record command/time/result in ops notes.

Related stories: `US-0034`, `US-0035`, `US-0036`

## 10) Quick incident severity guide

- `SEV-1`: data-loss risk, write-path outage, persistent health-check failure.
- `SEV-2`: high queue failure rate, severe query latency, broad ingestion delays.
- `SEV-3`: isolated step/plugin failures, minor ranking quality regressions.

For active incidents, follow: [`../operations/runbook.md`](../operations/runbook.md)

## 11) Why did an importer run fail or import zero items?

Validate source and auth inputs first:

```bash
ctxt import chrome --file ./bookmarks.html --dry-run
ctxt import chrome --file ./bookmarks.html --max-items 10
```

Common causes:

1. Wrong export format (for example, non-Netscape bookmarks HTML).
2. Expired OAuth tokens for cloud sources.
3. Incremental checkpoint mismatch after source-side changes.
4. Dedup behavior skipping items already ingested.

Triage sequence:

1. Re-run with `--dry-run` to validate parsing.
2. Lower scope with `--max-items` for deterministic debug runs.
3. Validate server reachability (`/health`) and enqueue path behavior.
4. Check job list and logs for downstream pipeline failures.

Related stories: `US-0300` to `US-0317`, `US-0106`, `US-0113`

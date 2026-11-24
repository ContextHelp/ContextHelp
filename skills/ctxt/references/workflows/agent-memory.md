# Workflow: Agent Memory & Deterministic-First Retrieval

Use `ctxt` as a reproducible, provenance-tracked memory substrate across agent sessions.

For exact flags, follow the linked command sheets rather than guessing.
For tag/mention conventions and RSQL presets, see [memory-taxonomy.md](memory-taxonomy.md).

## Preconditions

Verify `dpkms` is reachable before ingesting or composing:

```bash
dpkms healthcheck
ctxt status --format json
```

If not running, start the worker: [../dpkms/serve.md](../dpkms/serve.md).

## The 7-Step Retrieval & Write-Back Contract

Run in order:

1. **Ingest** source context (`ctxt capture` with `--wait`, or async + optional `--ttl`/`--encrypt`). See [../ctxt/capture.md](../ctxt/capture.md).
2. **Confirm completion** before reading or composing (`ctxt job status <id>` / `ctxt job list` / `dpkms job log <id>`). Never compose while ingestion jobs are still `pending` or `running`. See [../ctxt/job.md](../ctxt/job.md), [../dpkms/job.md](../dpkms/job.md).
3. **Retrieve deterministically first** (`ctxt list` with structured flags or `--q` RSQL). See [../ctxt/list.md](../ctxt/list.md), [../ctxt/query-language.md](../ctxt/query-language.md).
4. **Fall back to hybrid/semantic search** only when structured retrieval misses recall (`ctxt find`). See [../ctxt/find.md](../ctxt/find.md).
5. **Inspect top objects** (`ctxt resolve <id> --format json` for body + provenance, or `ctxt show <id> --format json`) before synthesizing. See [../ctxt/resolve.md](../ctxt/resolve.md), [../ctxt/show.md](../ctxt/show.md).
6. **Compose with citations** (`ctxt compose <type> ...`) keeping source object IDs intact. See [../ctxt/compose.md](../ctxt/compose.md).
7. **Write back a Memory Record** capturing hypothesis, attempts, failure modes, and decision so future agents reuse it.

## Canonical Playbook

### 1. Ingest and wait

```bash
ctxt capture "Investigate checkout funnel anomalies" \
  --type text --hint "#checkout,#funnel" --mention "@project.checkout-redesign" --wait --format json

ctxt capture https://example.com/incident-postmortem --type url --wait --format json
```

If enqueued without `--wait`, poll jobs:

```bash
ctxt job list --limit 20 --format json
ctxt job status <job-id> --format json
dpkms job log <job-id>
ctxt job retry <job-id>
```

### 2. Deterministic retrieval first

```bash
ctxt list --type url --since 2026-02-01 --limit 20 --format json
ctxt list --q "type==url;tag=in=(checkout,pricing)" --limit 20 --format json
ctxt list --mention @project.checkout-redesign --since 2026-02-01 --limit 20 --format json
```

### 3. Semantic/hybrid fallback second

```bash
ctxt find "recent checkout architecture decisions" --limit 10 --explain --format json
```

### 4. Resolve, inspect, and compose

```bash
ctxt resolve <object-id> --format json
ctxt compose brief --tagged checkout,funnel --since 2026-02-01 --output-file agent-brief.md
ctxt compose plan --mention @project.checkout-redesign --since 2026-02-01 --output-file agent-plan.md
```

## Shared Memory Record Contract

After each non-trivial task, write back one structured memory object:

```text
Memory Record
Task: <what was attempted>
Context: <system/user constraints>
Hypothesis: <expected outcome>
Actions Tried: <ordered attempts>
Observed Result: <actual outcome>
Failure Mode: <error/why it failed, or "none">
Fix or Decision: <what changed>
Confidence: <low|medium|high>
Source Object IDs: <id1,id2,...>
Next Reuse Hint: <when another agent should reuse this>
```

Persist with `ctxt capture` (use `--ttl` for ephemeral session notes or `--encrypt` for sensitive records):

```bash
ctxt capture --type text \
  --hint "#insight,#trial,#error,#decision,#lesson" \
  --mention "@agent.reviewer,@team.platform" --wait \
  "Memory Record
Task: Improve checkout conversion query quality
Context: release-week, high auditability requirement
Hypothesis: deterministic filters outperform broad semantic search
Actions Tried: 1) ctxt find broad query 2) ctxt list --q with tag/date constraints
Observed Result: deterministic query returned stable top results
Failure Mode: semantic-only query produced irrelevant matches
Fix or Decision: adopt deterministic-first retrieval policy
Confidence: high
Source Object IDs: obj_123,obj_456
Next Reuse Hint: reuse for incident reports and recurring summaries"
```

## Quality Gates

Treat a retrieval loop as complete only when all checks hold:
1. Deterministic `ctxt list` query is reproducible across repeated runs.
2. Returned objects match intended tags, mentions, and date window.
3. Composed outputs cite verified source object IDs.
4. At least one reusable `Memory Record` is written back.

## Failure Handling

- **Empty deterministic result**: drop one filter at a time; verify corpus presence with `ctxt list --limit 10`.
- **Ingestion/composition race**: verify no jobs are still running via `ctxt job list --state running`.
- **Noisy semantic hits**: lower `--limit`, add metadata facets (`--meta-type`, `--topic`, `--person`), or inspect weights with `--explain`.
- **Avoid over-constraining**: do not duplicate a tag filter in both `--tagged` and `--q` unless intentional — both compound.

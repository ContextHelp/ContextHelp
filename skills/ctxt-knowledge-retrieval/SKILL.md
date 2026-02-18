---
name: ctxt-knowledge-retrieval
description: Train and prompt AI agents to use ctxt for deterministic knowledge retrieval with semantic fallback, provenance tracking, and reusable memory capture. Use when building agent loops over ctxt, debugging weak retrieval quality, or sharing insights, trial-and-error outcomes, and decisions across agents and sessions.
---

# ctxt Knowledge Retrieval

## Objective

Use `ctxt` as an agent memory and retrieval substrate with reproducible queries, explicit provenance, and continuous write-back of learned experience.

## Canonical Sources

Use these docs as source of truth before changing agent behavior:

- `docs/manual/personas/agents-llms.md`
- `docs/manual/workflows/search-retrieval.md`
- `docs/manual/workflows/ingestion-capture.md`
- `docs/manual/workflows/composition-reporting.md`
- `docs/manual/reference/query-language-and-ranking.md`
- `docs/manual/reference/api-cli-reference.md`

## Runtime Preconditions

Start the runtime and verify readiness:

```bash
dpkms serve
ctxt version
curl http://127.0.0.1:8080/health
```

## Agent Retrieval Contract

Run this loop in order for reliable retrieval:

1. Ingest (`ctxt analyze`).
2. Poll jobs to completion (`ctxt job status` / `ctxt job list`).
3. Retrieve with deterministic filters first (`ctxt list`, `ctxt list --q`).
4. Use semantic search only as fallback (`ctxt find`).
5. Inspect top objects (`ctxt open`) before composing.
6. Compose outputs (`ctxt make ...`) and keep source IDs.
7. Write back new memory from task outcomes.

## Command Playbook

### Ingest

```bash
ctxt analyze "Investigate checkout funnel anomalies" --type text --hints "#checkout #funnel"
ctxt analyze https://example.com/incident-postmortem --type url
ctxt job list --limit 20
ctxt job status <job_id>
ctxt job log <job_id>
ctxt job retry <job_id>
```

### Deterministic retrieval first

```bash
ctxt list --type url --after 2026-02-01 --limit 20
ctxt list --q "type==url;tag=in=(checkout,pricing)" --limit 20
ctxt list --mention @project.checkout-redesign --after 2026-02-01 --limit 20
```

### Semantic fallback second

```bash
ctxt find "recent checkout architecture decisions" --limit 10
```

### Inspect and compose

```bash
ctxt open <object_id> --output json
ctxt make brief --tag checkout,funnel --since 2026-02-01 --output-file agent-brief.md
ctxt make plan --mention @project.checkout-redesign --since 2026-02-01 --output-file agent-plan.md
```

## Shared Memory Contract (Insights + Trial-and-Error)

After each meaningful task, persist one memory object that captures what worked, what failed, and why.

Use this template:

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

Write it back:

```bash
ctxt analyze --type text --hints "#insight #trial #error #decision #lesson" --mentions "@agent.codex @team.platform" "Memory Record
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

Retrieve shared memory later:

```bash
ctxt list --q "type==text;tag=in=(insight,trial,error,decision,lesson)" --after 2026-02-01 --limit 50
ctxt find "lessons learned from failed retrieval attempts" --limit 10
ctxt make brief --tag insight,trial,error,decision --since 2026-02-01 --output-file agent-memory-brief.md
```

## Quality Gates

Treat retrieval as acceptable only when all checks pass:

1. Deterministic query is reproducible across repeated runs.
2. Top results map to the intended tags/mentions/time window.
3. Composition output can be traced to source object IDs.
4. At least one reusable memory record is written for future agents.

## Failure Handling

- Empty deterministic result: remove one filter at a time, confirm objects exist with simpler `ctxt list`.
- Ingestion/composition race: never compose before jobs reach `completed`.
- Ambiguous semantic matches: reduce limit and add structured filters.
- Runtime/story drift: follow current CLI/API docs first; treat story contracts as forward-looking.

## Resources

Use `references/ctxt-memory-taxonomy.md` for standard tags, mention conventions, and query presets for cross-agent memory sharing.

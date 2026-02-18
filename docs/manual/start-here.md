# Start Here

This page gives a single onboarding path before you branch into role-specific manuals.

## 1. Pick your deployment model

- `ctxt`: end-user capture, search, and composition interface
- `dpkms (self-hosted)`: local or enterprise-managed substrate
- `dpkms cloud`: managed substrate with reduced ops overhead

Reference: [`../stories/README.md`](../stories/README.md)

## 2. Verify your runtime

Check binaries:

```bash
ctxt version
dpkms version
```

Start server:

```bash
dpkms serve
```

Run this in a dedicated terminal so you can use `ctxt` commands in another shell.

Optional health check:

```bash
curl http://127.0.0.1:8080/health
```

## 3. Run a baseline end-to-end test

1. Ingest and capture content
2. Enrich and structure content
3. Search and retrieve context
4. Compose outputs (briefs, plans, reports)

Example:

```bash
echo "Checkout drop-off increased after pricing change" | ctxt analyze --type text --hints "#checkout #pricing"
ctxt analyze https://example.com/pricing-analysis --type url
ctxt job list
ctxt find "pricing and checkout decisions" --limit 5
ctxt make brief --tag pricing,checkout --since 2026-02-01 --output-file first-brief.md
```

If ingestion is still running, inspect:

```bash
ctxt job status <job_id>
ctxt job log <job_id>
```

Optional importer smoke test:

```bash
ctxt import chrome --file ./bookmarks.html --dry-run
```

Reference: [`workflows/README.md`](./workflows/README.md)

## 4. Choose your role path

- Knowledge Worker: [`personas/knowledge-workers.md`](./personas/knowledge-workers.md)
- Agent or LLM Tool: [`personas/agents-llms.md`](./personas/agents-llms.md)
- Platform Integrator: [`personas/platform-integrators.md`](./personas/platform-integrators.md)
- Operations: [`personas/operations.md`](./personas/operations.md)
- Maintainer: [`personas/maintainers.md`](./personas/maintainers.md)

## 5. Choose your task path

- Ingest content: [`workflows/ingestion-capture.md`](./workflows/ingestion-capture.md)
- Validate extraction: [`workflows/enrichment-processing.md`](./workflows/enrichment-processing.md)
- Retrieve context: [`workflows/search-retrieval.md`](./workflows/search-retrieval.md)
- Produce artifacts: [`workflows/composition-reporting.md`](./workflows/composition-reporting.md)

## 6. Operational and extension paths

- Setup/security and governance: [`admin-extensibility/admin-configuration.md`](./admin-extensibility/admin-configuration.md)
- Pipeline lifecycle and extensibility: [`admin-extensibility/pipeline-management.md`](./admin-extensibility/pipeline-management.md)
- Plugin development: [`admin-extensibility/plugin-development.md`](./admin-extensibility/plugin-development.md)
- Reliability/scaling: [`operations/runbook.md`](./operations/runbook.md)
- API and CLI reference: [`reference/api-cli-reference.md`](./reference/api-cli-reference.md)
- Query and ranking reference: [`reference/query-language-and-ranking.md`](./reference/query-language-and-ranking.md)

## 7. Quick acceptance checklist

- `dpkms serve` starts cleanly.
- At least one analyze job reaches `completed`.
- `ctxt find` returns relevant results.
- `ctxt make brief` produces a reusable artifact.
- You know which persona and workflow chapter you will use next.

Story anchors:
- `US-0001`, `US-0002`, `US-0012`, `US-0016`, `US-0017`, `US-0022`, `US-0025`, `US-0300`, `US-0301`

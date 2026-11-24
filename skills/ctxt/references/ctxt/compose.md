# `ctxt compose`

Generate LLM-synthesized compositions (`brief`, `plan`, `summary`, or `draft`) from filtered knowledge objects with inline `[ref:ID]` citations.

## Execution Profile

- **Side effect**: `write`
- **Idempotency**: `no` — LLM-backed generation is non-deterministic across runs.

## Usage & Flags

```bash
ctxt compose <brief|plan|summary|draft> [flags]
```

| Flag | Default | Purpose |
|---|---|---|
| `--tagged <expr>` | `[]` | Tag filter (`+required,boost,-excluded`; repeatable, e.g. `--tagged +a,b,+c`) |
| `--mention <ref>` | `""` | Focus on one mention (`@ns.slug` or `ctxt://` URI) |
| `--since <expr>` | `""` | Start window (ISO date or relative duration, e.g. `30d`) |
| `--until <expr>` | `""` | End window (ISO date or relative duration) |
| `--output-file <path>` | `""` | Write composition to file |
| `--export <fmt>` | `markdown` | Output format: `markdown` or `json` (includes structured citations) |
| `--no-citations` | `false` | Disable inline `[ref:ID]` citations |

## Agent Recipes

```bash
# Executive brief requiring tags a and c, ranking b first, last 30 days
ctxt compose brief --tagged +a,b,+c --since 30d --export json

# Action plan scoped to a mention
ctxt compose plan --mention @project.signup-redesign --output-file plan.md

# Draft exported as JSON with structured citations
ctxt compose draft --tagged launch --output-file launch.json --export json
```

## Gotchas & Next Steps

- Confirm ingestion jobs finished before composing (`ctxt job list --state running`, [job.md](job.md)).
- Keep citations enabled in agent workflows so claims trace to source object IDs.
- Next: verify underlying context ([find.md](find.md), [list.md](list.md)) or inspect output ([show.md](show.md)).

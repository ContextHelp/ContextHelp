# `ctxt compose`

Generate LLM-synthesized compositions (`brief`, `plan`, `summary`, or `draft`) from filtered knowledge objects with inline `[ref:ID]` citations. Replaces legacy `make`.

## Execution Profile

- **Side effect**: `write` (generates and stores a composition)
- **Idempotency**: `no` — LLM-backed generation is non-deterministic across runs.

## Usage

```bash
ctxt compose <type> [flags]
```

Supported `<type>` values:
- `brief` — executive brief
- `plan` — action plan
- `summary` — synthesized summary
- `draft` — publish-ready draft

## Key Flags

| Flag | Default | Purpose |
|---|---|---|
| `--tagged <csv>` | `""` | Focus on specific tags (comma-separated) |
| `--mention <@slug>` | `""` | Focus on specific mentions |
| `--since <expr>` | `""` | Start window: ISO date or natural language (`"yesterday"`, `"2 hours ago"`) |
| `--until <expr>` | `""` | End window: ISO date or natural language (`"now"`, `"yesterday evening"`) |
| `--session <sess_id>` | `""` | Scope to one work session (`sess_xxx`) |
| `--output-file <path>` | `""` | Write composition output to file |
| `--export <fmt>` | `markdown` | Output format: `markdown` or `json` (includes structured citations) |
| `--no-citations` | `false` | Disable inline `[ref:ID]` citations |

## Agent Recipes

```bash
# Executive brief from tagged knowledge with structured citations
ctxt compose brief --tagged ux,onboarding --export json

# Action plan scoped to a project mention and date window
ctxt compose plan --mention @project.signup-redesign --since 2026-01-01 --output-file plan.md

# Summary for a natural-language window
ctxt compose summary --since "yesterday afternoon" --until now

# Brief scoped to a single work session
ctxt compose brief --session sess_a1b2c3d4e5f6 --output-file session-brief.md
```

## Gotchas & Next Steps

- **Never compose before ingestion jobs finish**: confirm with `dpkms job list --state running` ([../dpkms/job.md](../dpkms/job.md)).
- Keep citations enabled (`--no-citations` off) in agent workflows so every claim traces to source object IDs.
- Next: verify available context ([find.md](find.md), [list.md](list.md)) or inspect the generated object ([show.md](show.md)).

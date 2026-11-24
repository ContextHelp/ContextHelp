# Memory Taxonomy & Query Presets

Standard hints, mentions, and verified RSQL presets for cross-session agent memory sharing.
For full RSQL syntax and supported fields, see [../ctxt/query-language.md](../ctxt/query-language.md).

## Standard Memory Hints / Tags

Attach via `--hint` during capture so enrichment preserves consistent labels:

- `#insight` — durable learning or reusable pattern
- `#trial` — experiment or attempt worth preserving
- `#error` — failure mode, bug, or dead end
- `#decision` — selected path with rationale
- `#lesson` — concise do/don't rule for future runs

Apply multiple hints as needed (`--hint "#decision,#lesson"`). Pair with domain-specific hints rather than generic tags alone.

## Mention Conventions

Scope ownership and retrieval with `@namespace.slug`:

| Namespace | Convention | Example |
|---|---|---|
| Project | `@project.<slug>` | `@project.checkout-redesign` |
| Team | `@team.<slug>` | `@team.platform` |
| Agent | `@agent.<name>` | `@agent.reviewer` |
| Domain / system | `@domain.<slug>` | `@domain.payments` |

## Verified RSQL Query Presets

> Note: In `--q` RSQL, `mention` uses `==` (not `:`) and logical AND is `;`.

### Recent lessons, insights, and decisions for one project

```bash
ctxt list \
  --q "type==text;tag=in=(lesson,insight,decision);mention==project.checkout-redesign" \
  --since 2026-02-01 --limit 30 --format json
```

### Failure and trial history only

```bash
ctxt list \
  --q "type==text;tag=in=(error,trial)" \
  --since 2026-02-01 --limit 50 --format json
```

### Handoff brief scoped to an agent

```bash
ctxt list \
  --q "type==text;tag=in=(insight,trial,error,decision,lesson);mention==agent.reviewer" \
  --since 2026-02-01 --limit 50 --format json

ctxt compose brief \
  --tagged insight,trial,error,decision,lesson \
  --mention @agent.reviewer \
  --since 2026-02-01 \
  --output-file agent-handoff-brief.md
```

### 1-hop related objects around an entity

```bash
ctxt list --q "related==@project.checkout-redesign" --limit 25 --format json
```

## Minimum Memory Record Fields

Include these fields when persisting a Memory Record:

1. `Task`
2. `Hypothesis`
3. `Actions Tried`
4. `Observed Result`
5. `Failure Mode` (or `none`)
6. `Fix or Decision`
7. `Source Object IDs`
8. `Next Reuse Hint`

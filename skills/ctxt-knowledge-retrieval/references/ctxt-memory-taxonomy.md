# ctxt Memory Taxonomy

Use this taxonomy to make agent memory retrieval consistent across sessions and agents.

## Standard Tags

- `#insight`: durable learning or pattern.
- `#trial`: an attempt worth preserving.
- `#error`: failure mode, bug, or dead end.
- `#decision`: selected path with rationale.
- `#lesson`: concise "do/don't" for future execution.

Apply multiple tags when needed. Prefer specific tags over broad generic tags.

## Mention Conventions

Use mentions to scope retrieval and ownership:

- Project: `@project.<slug>`
- Team: `@team.<slug>`
- Agent: `@agent.<name>`
- System/domain: `@domain.<slug>`

Examples:

- `@project.checkout-redesign`
- `@team.growth`
- `@agent.codex`

## Query Presets

### Retrieve recent lessons for one project

```bash
ctxt list --q "type==text;tag=in=(lesson,insight,decision);mention:project.checkout-redesign" --after 2026-02-01 --limit 30
```

### Retrieve failure history only

```bash
ctxt list --q "type==text;tag=in=(error,trial)" --after 2026-02-01 --limit 50
```

### Retrieve by agent and summarize handoff

```bash
ctxt list --q "type==text;tag=in=(insight,trial,error,decision,lesson);mention:agent.codex" --after 2026-02-01 --limit 50
ctxt make brief --tag insight,trial,error,decision,lesson --since 2026-02-01 --output-file codex-handoff-brief.md
```

## Minimum Memory Record Fields

When writing memory records, include these fields:

1. `Task`
2. `Hypothesis`
3. `Actions Tried`
4. `Observed Result`
5. `Failure Mode` (or `none`)
6. `Fix or Decision`
7. `Source Object IDs`
8. `Next Reuse Hint`

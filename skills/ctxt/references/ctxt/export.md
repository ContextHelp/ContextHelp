# `ctxt export`

Export a single knowledge object through a loaded output-generator plugin.

- **Use `export`** when an output-generator plugin needs a `--dest` target path hint.
- **Use `show --format <plugin>`** for stdout-only plugin rendering ([show.md](show.md)).

## Execution Profile

- **Side effect**: `read` (plugin may write to `--dest` if supported)
- **Idempotency**: `yes`

## Usage

```bash
ctxt export [id] [flags]
```

## Key Flags

| Flag | Default | Purpose |
|---|---|---|
| `--dest <path>` | `""` | Destination path hint passed to the output-generator plugin |

Combine with global `--format <plugin-name>` to select the generator plugin.

## Agent Recipes

```bash
# Export an object via a loaded generator plugin
ctxt export obj_12345678 --format markdown --dest ./out/note.md
```

## Next Steps

- Inspect raw object fields: [show.md](show.md) · Synthesize across many objects: [compose.md](compose.md)

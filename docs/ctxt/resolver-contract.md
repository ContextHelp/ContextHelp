# Resolver contract (`ctxt resolve`)

`ctxt resolve <ref>` is the one-shot resolution contract for **external consumers** — agents, scripts, editors, CI steps — that need a single knowledge item's body and provenance without driving the interactive surfaces (`show`, `find`, `entity show`). It is a thin wrapper over the existing retrieval paths; it adds no retrieval logic of its own.

## Invocation

```bash
ctxt resolve <ref> [--format md|json]
```

One ref in, one result out, exit. No prompts, no pagination, no clipboard fallback.

## Ref forms

| Form | Resolves via | Notes |
|---|---|---|
| `obj_<id>` | knowledge-object store (`GetObject`) | exact ID match |
| `@<slug>` | entity store, alias-aware resolve | leading `@` stripped; matches slug or any alias |
| `<slug>` | entity store, alias-aware resolve | same as `@<slug>` — the `@` is optional |

Dispatch is prefix-based: refs starting with `obj_` are objects; everything else is treated as an entity ref. Entity resolution tries exact slug first, then aliases — the same resolution used for `@mention` handling.

## Output formats

### `md` (default)

Markdown to stdout, pipe-friendly:

- Objects: `# <first summary>` (when present) followed by the document body — graph-projected sections when available, else flat text content, else raw content.
- Entities: `# <title>` followed by the description.

The kit-owned persistent `--format` flag defaults to `table`; `resolve` treats `table` (unset) and `md` identically as markdown.

### `json`

A single envelope with body **and provenance**:

```json
{
  "ref": "obj_12345678",
  "kind": "object",
  "title": "Progressive disclosure in signup",
  "body": "Signup flows should use progressive disclosure.",
  "provenance": {
    "source": "import:obsidian:notes/signup.md",
    "pipeline": "text.short",
    "content_hash": "abc123def456",
    "registry_influences": ["uxpatterns"],
    "created_at": "2026-07-23T09:00:00Z",
    "updated_at": "2026-07-23T09:00:00Z"
  }
}
```

## Provenance fields

| Field | Kind | Meaning |
|---|---|---|
| `source` | object | Ingestion origin (URL, `import:<provider>:<path>`, capture source) |
| `pipeline` | object | Pipeline that enriched the object |
| `content_hash` | object | SHA-256 dedup hash of normalized content |
| `registry_influences` | object | Registry names whose semantics shaped enrichment |
| `registry_url` | entity | Registry the entity was synced from (empty for local entities) |
| `version_hash` | entity | Registry version hash of the entity record |
| `namespace` | entity | Entity namespace |
| `content_status` | entity | `full` \| `thin` \| `pending_pull` — see below |
| `created_at` / `updated_at` | both | Record timestamps (always present) |

Empty string and slice provenance fields are omitted from the JSON (`omitempty`); consumers must treat absence as "locally produced / not registry-derived". `created_at` / `updated_at` are always emitted, zero-valued when unset.

A `thin` or `pending_pull` entity is an index-only stub synced from a registry: `body` is legitimately empty until the content is pulled, and resolve also warns on stderr. Consumers must distinguish this from `full` with an empty body — retrying a `thin` ref returns the same empty body indefinitely.

## Exit codes

Follows the CLI-wide convention:

| Code | Meaning |
|---|---|
| `0` | Ref resolved; body written to stdout |
| `1` | Any error, including ref not found and an unsupported `--format` (message on stderr; JSON errors carry `CTXT-XXXX` codes per [../errors.md](../errors.md)) |
| `2` | CLI validation failure (kit strict-gate / flag parsing) |

## Guarantees

- **Read-only** (`kit/side-effect: read`) and **idempotent** — safe to call in retry loops. Retrying does not hydrate content: a `thin` entity resolves to the same empty body every time, so check `content_status` rather than looping.
- **Formats are markdown (default) or `json`** — any other `--format` is rejected with exit 1 rather than silently falling back.
- **One-shot** — no daemon required beyond the storage backend the CLI already uses; no watch mode.
- **Stable envelope** — `ref`, `kind`, `body`, `provenance` are the contract surface; new provenance fields may be added, existing ones will not be renamed.

## See also

- [api-cli.md](api-cli.md) — full CLI reference
- [schema-object.md](schema-object.md) — knowledge object schema
- [knowledge-directory.md](knowledge-directory.md) — file-source pattern producing resolvable objects
- [../dpkms/registries.md](../dpkms/registries.md) — where registry provenance originates

# ContextHelp Catalog Specification (v1)

`catalog.json` is the catalog entry for an extension or skill contributed to
this repository. It is discovery metadata for people and AI agents: what the
contribution is, what it needs, what it offers, and how to find it.

Neither `ctxt` nor `dpkms` reads these files. Declaring something in
`provides` does not register it anywhere; it only describes it.

## File location

Every extension and skill MUST contain a `catalog.json` in its root directory:

- `extensions/<slug>/catalog.json`
- `skills/<slug>/catalog.json`

The schema is [`.github/catalog.schema.json`](../.github/catalog.schema.json).
`scripts/catalog_test.go` validates every `catalog.json` under `extensions/`
and `skills/` against it, and fails if a contribution still carries a
`manifest.json`. Unknown fields are rejected.

## Fields

### Identity (required)

- `name`: human-readable name.
- `slug`: URL-safe identifier, `^[a-z0-9-]+$` (e.g. `raycast-extension`).
- `description`: one-line summary.
- `version`: `MAJOR.MINOR.PATCH` (e.g. `1.0.0`).
- `category`: one of `extensions`, `skills`, `schemas`.
- `author` (required): object with `name` and optional `github`.

### Requires (required)

What a person or agent needs before using the contribution.

- `requires.ctxt` (required): always `true`.
- `requires.dpkms`: `true` if it needs a running dpkms.
- `requires.services`: external services (e.g. `["OpenAI"]`).
- `requires.tools`: CLI tools or apps (e.g. `["ffmpeg", "Raycast"]`).
- `requires.permissions`: access it expects (e.g. `["filesystem.read"]`).

### Provides

What the contribution offers, for browsing and search.

- `provides.pipelines`: pipeline ids it ships.
- `provides.commands`: commands it adds or relies on.
- `provides.schemas`: knowledge or entity schemas it ships.
- `provides.triggers`: free-form object describing when it applies (e.g.
  `{"mime_types": ["audio/mpeg"]}`).

### Discovery

- `tags` (required): at least one browsing keyword.
- `difficulty`: `beginner`, `intermediate` or `advanced`.
- `icon`: relative path to an SVG or PNG icon.

## Example

```json
{
  "name": "Local Audio Processor",
  "slug": "local-audio-proc",
  "description": "Transcribes audio with local Whisper models.",
  "category": "extensions",
  "version": "1.0.0",
  "author": { "name": "Jadb" },
  "requires": {
    "ctxt": true,
    "tools": ["ffmpeg"]
  },
  "provides": {
    "pipelines": ["audio-high-fidelity"],
    "triggers": { "mime_types": ["audio/mpeg", "audio/wav"] }
  },
  "tags": ["audio", "transcription", "local-first"],
  "difficulty": "intermediate"
}
```

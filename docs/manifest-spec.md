# ContextHelp Manifest Specification (v1)

The `manifest.json` file is the foundational declaration for any modular component in the ContextHelp ecosystem. It serves two purposes:
1. **Discovery:** Providing metadata for humans and AI agents.
2. **Wiring:** Defining functional integration points for the `ctxt` and `dpkms` engines.

## File Location
Every module MUST contain a `manifest.json` in its root directory.
- `extensions/<slug>/manifest.json`
- `plugins/<slug>/manifest.json`
- `skills/<slug>/manifest.json`

## Schema Structure

### 1. Identity (Required)
- `name`: Human-readable name.
- `slug`: Unique URL-safe identifier (e.g., `raycast-extension`).
- `description`: Brief summary of functionality.
- `version`: Semantic versioning (e.g., `1.0.0`).
- `category`: One of `extensions`, `plugins`, `skills`, `schemas`.

### 2. Attribution
- `author`: Object containing `name` and optional `github`.
- `license`: SPDX license identifier (e.g., `AGPL-3.0`).

### 3. Requirements (The "Needs")
- `requires`: 
    - `ctxt`: Boolean (True if it needs the brain).
    - `dpkms`: Boolean (True if it needs the substrate).
    - `services`: Array of external APIs (e.g., `["OpenAI", "Stripe"]`).
    - `tools`: Array of CLI tools (e.g., `["ffmpeg", "yt-dlp"]`).
    - `permissions`: Array of engine capabilities (e.g., `["filesystem.read", "network.outbound"]`).

### 4. Capabilities (The "Provides")
This section defines how the module plugs into the engine.
- `provides`:
    - `pipelines`: Array of pipeline IDs defined by this module.
    - `commands`: Array of CLI command extensions.
    - `schemas`: Array of knowledge/entity schemas provided.
    - `triggers`: Conditions under which this module should activate (e.g., `{"file_ext": [".pdf"]}`).

### 5. Metadata
- `tags`: Browsing keywords.
- `difficulty`: `beginner`, `intermediate`, `advanced`.
- `icon`: Relative path to an SVG/PNG icon.

## Example Manifest

```json
{
  "name": "Local Audio Processor",
  "slug": "local-audio-proc",
  "description": "High-fidelity audio transcription using local Whisper models.",
  "category": "plugins",
  "version": "1.0.0",
  "author": { "name": "Jadb" },
  "requires": {
    "ctxt": true,
    "tools": ["ffmpeg"],
    "services": ["OpenAI Whisper (Local)"]
  },
  "provides": {
    "pipelines": ["audio-high-fidelity"],
    "triggers": { "mime_types": ["audio/mpeg", "audio/wav"] }
  },
  "tags": ["audio", "transcription", "local-first"]
}
```

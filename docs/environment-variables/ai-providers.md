# AI Providers Environment Variables

API keys for the hosted LLM and vision providers, and the embedding
provider.

---

## API keys

Each key is looked up by its name in the active secrets store
(`secrets.backend`, set with `dpkms secret set <KEY>`) first, then in the
environment of the process that runs the step — usually `dpkms serve`.
When both hold a key, the secrets store wins. The provider factory
resolves the key once and hands it to the client, so the key that selects
a provider is the key it sends. **Never commit them to version control.**

| Env var | Used by |
|---------|---------|
| `OPENAI_API_KEY` | `openai` LLM, vision and summarization backends |
| `ANTHROPIC_API_KEY` | `anthropic` LLM, vision and summarization backends |
| `GEMINI_API_KEY` | `gemini` vision backend |
| `OPENROUTER_API_KEY` | `openrouter` vision backend |

```bash
OPENAI_API_KEY=sk-...
ANTHROPIC_API_KEY=sk-ant-...
```

**Security:** use secret management:
```bash
OPENAI_API_KEY=$(vault kv get -field=key secret/openai)
```

A key never selects a provider. `backend: auto` (the default) resolves to
a local backend only: the LLM to Ollama, vision to Ollama when it answers
and a vision model is configured, otherwise unavailable. A hosted backend
runs only when `providers.<role>.backend` (or a per-pipeline override in
`pipelines.overrides`) names it, so a key sitting in the environment sends
nothing anywhere until you opt in. A named hosted backend whose key does
not resolve is a configuration error naming the key; a named `ollama`
backend of `vision`, `llm` or `summarization` with no `model` is one too
(`providers.<role>.model`). `auto` never errors on either.

Vision `auto` needs the model to be installed: it must appear in
`ollama list` (`qwen2.5vl:7b` by default; `:latest` is implied for a bare
name). The check runs once per process, so restart `dpkms serve` after
pulling a model or starting Ollama.

Backend names are exact and case-sensitive: `Anthropic` is not
`anthropic`. `ctxt config validate` and `dpkms serve` reject an unknown
name and list the valid ones.

### Per-pipeline overrides

`pipelines.overrides.<pipeline>.providers.<role>` takes any role, `frames`
and `summarization` included, and is merged onto `providers.<role>` field
by field: a field the override leaves empty is inherited. A bare
`{backend: auto}` therefore keeps the base model and endpoint. The
exception is a backend change across the hosted/local line, or to a
different hosted backend (`ollama` to `anthropic`, `openai` to
`anthropic`, `anthropic` to `auto`): the override then drops the base's
`model`, `endpoint`, `language` and `num_ctx`, since values written for
one backend mean nothing to another. Settings that do not depend on the
backend (`prompt_template`, the frame selection keys) are still
inherited. Set `model` in the override when the new backend needs one, for
example `{backend: ollama, model: llama3.2}`. The merged config is
validated like the base: an override that leaves a pipeline's role
unavailable fails startup.

---

## Backend, model and endpoint

No env vars. Set them per provider kind (`llm`, `vision`, `ocr`,
`transcription`, `video`, `document`, `diarization`, `frames`,
`summarization`) in the config file, or
with `-c`:

```yaml
providers:
  llm:
    backend: auto                    # auto (= local ollama) | anthropic | openai | ollama | stub
    model: llama3                    # local default; a hosted backend left on it uses its own default
    endpoint: http://localhost:11434 # Ollama URL
  vision:
    backend: auto                    # auto (= local ollama) | ollama | openai | anthropic | gemini | openrouter | stub
    model: qwen2.5vl:7b
    endpoint: http://localhost:11434
```

The built-in defaults `llama3` (LLM) and `qwen2.5vl:7b` (vision) are local
Ollama models. A hosted backend left on its built-in default model uses its
own. LLM: `claude-haiku-4-5-20251001` (Anthropic), `gpt-4o-mini` (OpenAI).
Vision: `gpt-4o` (OpenAI), `claude-3-5-sonnet-20241022` (Anthropic),
`gemini-2.0-flash` (Gemini), `google/gemini-2.0-flash-lite` (OpenRouter). Ollama
has no model default beyond config: an empty `model` is a configuration error
(the call fails with "model not configured") and vision `auto` is unavailable.

```bash
dpkms serve -c providers.llm.backend=ollama -c providers.llm.model=mistral
```

### Frames

`providers.frames` selects the video frames a pipeline looks at. Scene
detection picks frames at visual cuts; the rate bounds shape the result.
No two frames are closer than `1/max_fps` seconds, and none further apart
than `1/min_fps` seconds (gaps are filled with evenly spaced frames).
Without scene detection, frames are evenly spaced at `min_fps`, or at
`max_fps` when `min_fps` is unset.

`max_frames` caps the result and wins over `min_fps`: a longer plan is
thinned to at most one frame per 1/`max_frames` of the video's length (a
scene cut wins its slot over a filler frame; the first slot keeps the frame
at 0), so a long video is sampled across its whole length and stays
bounded. Frames are scaled down to `max_dimension` pixels on the longest
side; `-1` turns scaling off. Each bound takes its default on its own:
setting only `max_fps` keeps the default `min_fps`, unless the default
would contradict it (a `max_fps` at or below 0.1 leaves `min_fps` unset,
and a `min_fps` at or above 1 leaves `max_fps` unset).

```yaml
providers:
  frames:
    backend: auto          # auto | ffmpeg | stub
    min_fps: 0.1           # default 0.1
    max_fps: 1             # default 1
    scene_detection: true  # default true
    scene_threshold: 0.3   # 0..1; 0 = backend default
    max_frames: 200        # default 200, at most 10000
    max_dimension: 1280    # default 1280, at most 16384; -1 = no scaling
```

`scene_threshold` is relative to the backend's own change score: the same
value can select different frames on different backends. Rates must be 0
(unset) or in (0, 1000], and `min_fps` and `max_fps` must leave a valid
spacing (`min_fps: 0.3` with `max_fps: 0.3` does not). `dpkms config
validate` checks the block.

`ffmpeg` is the local default. With `auto` and no ffmpeg installed, frames
are unavailable: `video.full` is not registered and answers `422
PIPELINE_NOT_FOUND` with the install hint, never placeholder frames. A
named `ffmpeg` that is missing is a configuration error at startup. Media
paths must be
absolute. A video with only cover art has no frames, and an unusable
duration in the file header fails the job without retries.

Extracted frames and the audio track of a video go to a scratch
directory, never beside the source. It is `ctxt/scratch` under your
cache directory: `$XDG_CACHE_HOME` or `~/.cache` on Linux,
`$XDG_CACHE_HOME` or `~/Library/Caches` on macOS, `%LOCALAPPDATA%` on
Windows. It is never under a shared temp dir.

The pipeline run removes its scratch files when it ends, on success and
on failure; a pipeline that hops to another pipeline cleans up after
each run. When the worker starts it also removes scratch older than a
day, left by a killed run.

On Linux and macOS the scratch root must be a plain directory (not a
symlink) owned by you. A root of yours with looser permissions is
tightened to `0700`. Anything else is refused with a message saying
what to fix, and the job fails without retries. The sweep skips entries
that are not plain directories. Windows reports no usable mode bits, so
the per-user `%LOCALAPPDATA%` permissions are the protection there.

### Summarization

`providers.summarization` produces summaries from a transcript or text.
`auto` and `ollama` use a local Ollama chat model; no model is built in, so
set `model`: without one, summarizing fails with "no model configured".
`openai` and `anthropic` run only when named here. An unknown backend is a
config error.

```yaml
providers:
  summarization:
    backend: auto          # auto | ollama | openai | anthropic | stub
    model: ""              # required for ollama; hosted backends have defaults
    endpoint: http://localhost:11434
    num_ctx: 0             # Ollama context window in tokens; 0 = the model's
    prompt_template: ""    # empty = built-in prompt per medium
```

Ollama's default context window is small and truncates a long transcript
from the front; set `num_ctx` to fit the transcripts you summarize. The
built-in prompts put their instructions after the text for the same reason.

`prompt_template` is a Go `text/template` with `.Text`, `.Medium`
(`meeting`, `video`, `audio` or empty) and `.Language`. It must place the
text with a plain `{{.Text}}` outside any `if`, `range` or `with`, and it
is run for every medium and language. `dpkms config validate` and `ctxt config
validate` reject a template that fails either check. A bad template that
is not validated first makes every summary fail with the template error;
nothing falls back to the built-in prompt.

---

## Embedding provider

Semantic and hybrid search (`ctxt find`), ingest, and re-embedding
(`ctxt embeddings migrate`, run by dpkms) use one embedding provider. Set
it once in config, and override it for a single run without editing
config.

`ctxt find` sends its query to dpkms, which embeds it with the settings
where `dpkms serve` runs (its `dpkms.yaml` and environment). ctxt's own
settings reach only the `ctxt embeddings` commands and `ctxt find --graph`.

This section is the settings reference. To turn semantic search on, see
[Turn on semantic search](../manual/workflows/semantic-search.md); to
switch or retire models, see
[Operate embedding models](../manual/operations/embeddings.md).

### Configure it

In `ctxt.yaml` / `dpkms.yaml`:

```yaml
providers:
  embedding:
    backend: ollama                  # ollama | stub
    model: snowflake-arctic-embed2
    endpoint: http://localhost:11434
    api_key_env: ""                  # NAME of the env var holding a key, never the key
    dimension: 1024                  # optional, informational; register measures it
```

Every field is optional. Unset fields fall back to `ollama`,
`nomic-embed-text` and `http://localhost:11434`. `dimension` is shown by
`ctxt embeddings provider` only: `ctxt embeddings register` always
measures the dimension from the provider, and checks it only against
`--dimension`.

### Override it for one run

Each setting is resolved on its own, highest first:

| Layer | Example |
|-------|---------|
| Flag (on `find --graph`, `embeddings provider`, `embeddings register`) | `--embedding-provider`, `--embedding-model`, `--embedding-endpoint` |
| Environment | `CTXT_EMBEDDING_PROVIDER`, `CTXT_EMBEDDING_MODEL`, `CTXT_EMBEDDING_ENDPOINT`, `CTXT_EMBEDDING_API_KEY_ENV` |
| `-c` override | `-c providers.embedding.model=snowflake-arctic-embed2` |
| Registered model | the model's endpoint and `api_key_env`, when a command targets a `model_id` |
| Config file | `providers.embedding` |
| Built-in default | `ollama`, `nomic-embed-text`, `http://localhost:11434` |

Overriding one setting keeps the others: `--embedding-endpoint` alone
still uses the model from env or config. The environment outranks `-c`
for these settings.

```bash
ctxt embeddings provider --embedding-model snowflake-arctic-embed2
CTXT_EMBEDDING_MODEL=snowflake-arctic-embed2 ctxt embeddings provider
ctxt embeddings provider -c providers.embedding.model=snowflake-arctic-embed2
```

### Use a remote Ollama for one run

Forward a local port to the remote machine's Ollama (port 11434), for
example with an SSH tunnel on local port 11555, then point the run at it:

```bash
ctxt find "onboarding" --graph --embedding-endpoint http://127.0.0.1:11555
CTXT_EMBEDDING_ENDPOINT=http://127.0.0.1:11555 dpkms serve   # ingest, embeddings migrate, find queries
```

The remote Ollama must have the model pulled.

### Check what a command will use

`ctxt embeddings provider` prints the resolved settings and the layer each
came from. It accepts the same flags, env and `-c` as the commands above,
and never prints a key value. With the config above and an endpoint flag:

```bash
ctxt embeddings provider
ctxt embeddings provider --embedding-endpoint http://127.0.0.1:11555 --format json
```

```json
{
  "model_id": "",
  "backend": "ollama",
  "model": "snowflake-arctic-embed2",
  "endpoint": "http://127.0.0.1:11555",
  "api_key_env": "",
  "dimension": 1024,
  "sources": {
    "api_key_env": "default",
    "backend": "config",
    "dimension": "config",
    "endpoint": "flag",
    "model": "config"
  },
  "fixed": []
}
```

Pass a registered `model_id` to see how a command targeting that model
resolves it, including the model's own registry settings:

```bash
ctxt embeddings provider ollama-snowflake-arctic-embed2@2026-09-26
```

A registered model's backend, model and dimension are fixed by its
registry entry and shown as "fixed by registry" (and listed under `fixed`
in JSON): its stored vectors came from that model. Flags, env and `-c` can still change its endpoint and
`api_key_env`, for example to reach it through a tunnel, but a value that
would change its backend, model or dimension fails the command
(`ctxt find --graph` answers from full-text search with a `provider_error`
notice instead).

`dpkms serve` prints the resolved provider at startup.

---

## Examples

### Hosted LLM (Anthropic)
```bash
ANTHROPIC_API_KEY=sk-ant-...
dpkms serve -c providers.llm.backend=anthropic
```

### Local only (Ollama)
```bash
dpkms serve -c providers.llm.backend=ollama -c providers.llm.model=mistral
```

---

## Related

- [../manual/workflows/semantic-search.md](../manual/workflows/semantic-search.md) — Turn on semantic search
- [../manual/operations/embeddings.md](../manual/operations/embeddings.md) — Operate embedding models
- [../ctxt/pipelines.md](../ctxt/pipelines.md) — Pipeline AI integration

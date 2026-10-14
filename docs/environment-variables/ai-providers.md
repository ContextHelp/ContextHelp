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
| `OPENAI_API_KEY` | `openai` LLM and vision backends |
| `ANTHROPIC_API_KEY` | `anthropic` LLM and vision backends |
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

With `backend: auto` (the default), a key found in either place also
selects the provider:
the LLM tries Anthropic, then OpenAI, then falls back to Ollama; vision
tries a local Ollama first, then OpenAI, Anthropic, Gemini, OpenRouter.

---

## Backend, model and endpoint

No env vars. Set them per provider kind (`llm`, `vision`, `ocr`,
`transcription`, `video`, `document`, `diarization`) in the config file, or
with `-c`:

```yaml
providers:
  llm:
    backend: auto                    # auto | anthropic | openai | ollama | stub
    model: ""                        # empty = backend default
    endpoint: http://localhost:11434 # Ollama URL
  vision:
    backend: auto                    # auto | ollama | openai | anthropic | gemini | openrouter | stub
    model: llava
    endpoint: http://localhost:11434
```

LLM defaults when `model` is empty: `claude-haiku-4-5-20251001`
(Anthropic), `gpt-4o-mini` (OpenAI), `llama3` (Ollama).

```bash
dpkms serve -c providers.llm.backend=ollama -c providers.llm.model=mistral
```

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
- [../scaling.md](../scaling.md#cost-optimization) — Cost optimization

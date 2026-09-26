# Workflow: Turn On Semantic Search

## Goal

Make `ctxt find` match by meaning, not only by words, using an embedding model you choose. Then point a single run at a different endpoint (a remote Ollama through a tunnel, for example) without touching config.

## Scope

- Choosing the embedding provider (`providers.embedding`)
- Registering a model, filling vectors for what you already captured, making it the default
- Overriding the provider for one run: flags, `CTXT_EMBEDDING_*` env, `-c`
- Reading the notice `ctxt find` prints when it falls back to full-text search
- Not covered: switching an existing instance to another model, retiring models, duplicate detection. Those are in [Operate embedding models](../operations/embeddings.md).

## Prerequisites

1. Ollama running where ctxt can reach it (default `http://localhost:11434`), with an embedding model pulled: `ollama pull nomic-embed-text`.
2. `dpkms serve` running for your instance. It embeds new captures and runs the background fill in Step 4.
3. Run the `ctxt embeddings` commands on a machine that can open the instance's database: they open it directly (`storage.path`, or the instance you pick with `--instance`), not through the server.

## Why `find` says "full-text only"

Until an embedding model is registered and made the default, every `ctxt find` prints:

```text
notice: semantic search unavailable (no_default_model): no default embedding model; register one with 'ctxt embeddings register' and make it the default; results are full-text only
```

The results are still correct full-text results. The steps below turn on the semantic half.

## Procedure

### Step 1: Choose the provider

With no configuration, ctxt uses Ollama at `http://localhost:11434` with `nomic-embed-text`. To pick something else, set it in `ctxt.yaml` (and in `dpkms.yaml`, since dpkms embeds your captures):

```yaml
providers:
  embedding:
    backend: ollama                  # ollama | stub
    model: snowflake-arctic-embed2
    endpoint: http://localhost:11434
```

Every field is optional; see [Embedding provider](../../environment-variables/ai-providers.md#embedding-provider) for all of them, including `api_key_env`.

### Step 2: Check what ctxt will use

```bash
ctxt embeddings provider
```

```text
Embedding provider

╭─────────────┬────────────────────────┬─────────╮
│ Setting     │ Value                  │ Source  │
├─────────────┼────────────────────────┼─────────┤
│ backend     │ ollama                 │ default │
│ model       │ nomic-embed-text       │ default │
│ endpoint    │ http://localhost:11434 │ default │
│ api_key_env │ (unset)                │ default │
│ dimension   │ (unknown)              │ default │
╰─────────────┴────────────────────────┴─────────╯
```

With no configuration every value is a `default`. The Source column names the layer each value came from: `flag`, `env`, `config-override` (`-c`), `registry`, `config` or `default`.

### Step 3: Register the model

A model ID names one model's vectors. The convention is `<backend>-<model>@<date>`:

```bash
ctxt embeddings register ollama-nomic-embed-text@2026-09-26
```

```text
Registered ollama-nomic-embed-text@2026-09-26

╭─────────────┬────────────────────────┬──────────╮
│ Setting     │ Value                  │ Source   │
├─────────────┼────────────────────────┼──────────┤
│ backend     │ ollama                 │ default  │
│ model       │ nomic-embed-text       │ default  │
│ endpoint    │ http://localhost:11434 │ default  │
│ api_key_env │ (unset)                │ default  │
│ dimension   │ 768                    │ measured │
╰─────────────┴────────────────────────┴──────────╯
Index: ready
Not the default model. Promote it with: ctxt embeddings set-default ollama-nomic-embed-text@2026-09-26
```

Register asks the provider for one embedding and records its length as the model's dimension, so the provider has to be reachable. From now on, every new capture is embedded with this model.

### Step 4: Embed what you already have

Captures from before Step 3 have no vectors yet. Queue the background fill:

```bash
ctxt embeddings migrate --to ollama-nomic-embed-text@2026-09-26
```

```text
Queued migration to ollama-nomic-embed-text@2026-09-26 (job 8405a93f-1349-47a8-ab5c-ddda8d91247e): 30 objects lack a row.
dpkms runs it in the background; it continues after this command exits.
Progress: ctxt upgrade status --watch
Coverage: ctxt embeddings list
```

dpkms does the work, so you can close the terminal. Check coverage until it reaches `1.00`:

```bash
ctxt embeddings list
```

```text
Embedding models (1)

╭────────────────────────────────────┬──────────┬─────┬─────────┬──────────┬──────────────────────┬────────────╮
│ Model ID                           │ Provider │ Dim │ Default │ Coverage │ Registered           │ Deprecated │
├────────────────────────────────────┼──────────┼─────┼─────────┼──────────┼──────────────────────┼────────────┤
│ ollama-nomic-embed-text@2026-09-26 │ ollama   │ 768 │         │ 1.00     │ 2026-09-26T19:55:53Z │            │
╰────────────────────────────────────┴──────────┴─────┴─────────┴──────────┴──────────────────────┴────────────╯
```

### Step 5: Make it the default

```bash
ctxt embeddings set-default ollama-nomic-embed-text@2026-09-26
```

```text
Default embedding model is now ollama-nomic-embed-text@2026-09-26 (was no default).
Coverage: 1.0000 (minimum 0.9900)
```

The next `ctxt find` searches by meaning as well as by words, and the notice is gone. No restart is needed, for the CLI or for dpkms. If the model covers less than 99% of your objects, set-default refuses and tells you to finish Step 4 first.

## Override the provider for one run

Every embedding setting is resolved on its own, and the highest layer wins: flag, then `CTXT_EMBEDDING_*` env, then `-c`, then the registered model's own settings, then the config file, then the default. These three runs are equivalent:

```bash
ctxt find "onboarding" --embedding-endpoint http://127.0.0.1:11555
CTXT_EMBEDDING_ENDPOINT=http://127.0.0.1:11555 ctxt find "onboarding"
ctxt find "onboarding" -c providers.embedding.endpoint=http://127.0.0.1:11555
```

The flags exist on `find`, `embeddings provider` and `embeddings register`. The env variables are `CTXT_EMBEDDING_PROVIDER`, `CTXT_EMBEDDING_MODEL`, `CTXT_EMBEDDING_ENDPOINT` and `CTXT_EMBEDDING_API_KEY_ENV`. Full table: [Override it for one run](../../environment-variables/ai-providers.md#override-it-for-one-run).

### Use a remote Ollama through a tunnel

Forward a local port to the remote machine's Ollama, then point one run at it:

```bash
ssh -N -L 11555:localhost:11434 gpu.example.com
```

In another terminal:

```bash
ctxt embeddings provider ollama-nomic-embed-text@2026-09-26 --embedding-endpoint http://127.0.0.1:11555
ctxt find "onboarding" --embedding-endpoint http://127.0.0.1:11555
```

```text
Embedding provider for ollama-nomic-embed-text@2026-09-26

╭─────────────┬────────────────────────┬───────────────────╮
│ Setting     │ Value                  │ Source            │
├─────────────┼────────────────────────┼───────────────────┤
│ backend     │ ollama                 │ fixed by registry │
│ model       │ nomic-embed-text       │ fixed by registry │
│ endpoint    │ http://127.0.0.1:11555 │ flag              │
│ api_key_env │ (unset)                │ default           │
│ dimension   │ 768                    │ fixed by registry │
╰─────────────┴────────────────────────┴───────────────────╯
```

The remote Ollama needs the same model pulled. For dpkms (new captures and the Step 4 fill), set the env on the server: `CTXT_EMBEDDING_ENDPOINT=http://127.0.0.1:11555 dpkms serve`. Its startup line shows the endpoint and `(env)`.

### Why a registered model's backend and model can't be overridden

A registered model's vectors all came from one model. Query vectors from any other model would not be comparable with them, so the results would be noise. The endpoint and `api_key_env` only change how ctxt reaches the model, so those stay overridable. The backend, model and dimension are fixed by the registry entry, and `ctxt embeddings provider <model_id>` labels them "fixed by registry".

An override that would change them makes the command fail, and `find` falls back to full-text with a notice:

```bash
ctxt find "onboarding" --embedding-model snowflake-arctic-embed2
```

```text
notice: semantic search unavailable (provider_error): model ollama-nomic-embed-text@2026-09-26: embedding model ollama-nomic-embed-text@2026-09-26 is registered with model=nomic-embed-text; --embedding-model=snowflake-arctic-embed2 cannot change it (only endpoint and api_key_env may be overridden for a registered model); results are full-text only
```

To use another model, register it under its own model ID and move to it: [Switch to another model](../operations/embeddings.md#switch-to-another-model).

## Outputs to validate

- `ctxt embeddings list` shows your model with `*` under Default and coverage at or near `1.00`.
- `ctxt find "<question>"` prints no `notice:` line.
- `ctxt find "<question>" --format json` reports `"status": "ok"` under `diagnostics.semantic`.

## Common failure modes

### `find` still prints a notice

The part in parentheses says why. `no_default_model` means Step 5 hasn't run. `provider_error` means the provider couldn't be reached or an override was refused. Every status and its fix: [Search fallback notice](../operations/embeddings.md#search-fallback-notice).

### Register fails with "probe … failed"

The provider could not be reached or doesn't have the model. Nothing was registered. Check `ctxt embeddings provider`, start Ollama or pull the model, and run register again.

### Coverage stays below `1.00`

Objects created outside an embedding pipeline, such as entity pages, get vectors only from `migrate`. Run `ctxt embeddings migrate --to <model_id>` again; it only embeds what is still missing.

## Related references

- [Operate embedding models](../operations/embeddings.md): switching models, migration status, retiring models, duplicates
- [Embedding provider settings](../../environment-variables/ai-providers.md#embedding-provider)
- [Search and retrieval](./search-retrieval.md)

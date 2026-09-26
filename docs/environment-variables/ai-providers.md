# AI Providers Environment Variables

Configuration for OpenAI, Anthropic, Ollama, and the embedding provider.

---

## OpenAI

### OPENAI_API_KEY
**Type:** string (secret)\
**Default:** _(none)_\
**Required:** For OpenAI models

OpenAI API key. **Never commit to version control.**

```bash
OPENAI_API_KEY=sk-...
```

**Security:** Use secret management:
```bash
OPENAI_API_KEY=$(vault kv get -field=key secret/openai)
```

### OPENAI_MODEL
**Type:** string\
**Default:** `gpt-4-turbo-preview`

Default OpenAI model for text generation.

```bash
OPENAI_MODEL=gpt-4-turbo-preview
```

**Available models:**
- `gpt-4-turbo-preview` — Latest GPT-4 Turbo
- `gpt-4` — GPT-4 base
- `gpt-3.5-turbo` — Fast, cost-effective

### OPENAI_MAX_TOKENS
**Type:** integer\
**Default:** `4096`

Maximum tokens per API call.

```bash
OPENAI_MAX_TOKENS=8192
```

### OPENAI_TEMPERATURE
**Type:** float (0.0-2.0)\
**Default:** `0.7`

Sampling temperature for generation.

```bash
OPENAI_TEMPERATURE=0.5
```

---

## Anthropic (Claude)

### ANTHROPIC_API_KEY
**Type:** string (secret)\
**Default:** _(none)_

Anthropic API key. **Never commit to version control.**

```bash
ANTHROPIC_API_KEY=sk-ant-...
```

### ANTHROPIC_MODEL
**Type:** string\
**Default:** `claude-3-opus-20240229`

Default Claude model.

```bash
ANTHROPIC_MODEL=claude-3-sonnet-20240229
```

**Available models:**
- `claude-3-opus-20240229` — Most capable
- `claude-3-sonnet-20240229` — Balanced
- `claude-3-haiku-20240307` — Fast, lightweight

### ANTHROPIC_MAX_TOKENS
**Type:** integer\
**Default:** `4096`

Maximum tokens per API call.

```bash
ANTHROPIC_MAX_TOKENS=8192
```

---

## Ollama (Local Models)

### OLLAMA_ENABLED
**Type:** boolean\
**Default:** `false`

Enable Ollama for local AI models.

```bash
OLLAMA_ENABLED=true
```

### OLLAMA_HOST
**Type:** string\
**Default:** `http://localhost:11434`

Ollama server URL.

```bash
OLLAMA_HOST=http://ollama.internal:11434
```

### OLLAMA_MODEL
**Type:** string\
**Default:** `llama2`

Default Ollama model.

```bash
OLLAMA_MODEL=mistral
```

**Available models (download first):**
- `llama2` — Meta's Llama 2
- `mistral` — Mistral AI
- `codellama` — Code-specialized
- `phi` — Microsoft Phi

---

## Embedding provider

Semantic and hybrid search (`ctxt find`) and re-embedding
(`dpkms dev reindex-vectors`) use one embedding provider. Set it once in
config, and override it for a single run without editing config.

### Configure it

In `ctxt.yaml` / `dpkms.yaml`:

```yaml
providers:
  embedding:
    backend: ollama                  # ollama | stub
    model: snowflake-arctic-embed2
    endpoint: http://localhost:11434
    api_key_env: ""                  # NAME of the env var holding a key, never the key
    dimension: 1024                  # optional; 0 or omitted = unknown
```

Every field is optional. Unset fields fall back to `ollama`,
`nomic-embed-text` and `http://localhost:11434`.

### Override it for one run

Each setting is resolved on its own, highest first:

| Layer | Example |
|-------|---------|
| Flag (on `find`, `embeddings provider`, `dpkms dev reindex-vectors`) | `--embedding-provider`, `--embedding-model`, `--embedding-endpoint` |
| Environment | `CTXT_EMBEDDING_PROVIDER`, `CTXT_EMBEDDING_MODEL`, `CTXT_EMBEDDING_ENDPOINT`, `CTXT_EMBEDDING_API_KEY_ENV` |
| `-c` override | `-c providers.embedding.model=snowflake-arctic-embed2` |
| Registered model | the model's own settings, when a command targets a `model_id` |
| Config file | `providers.embedding` |
| Built-in default | `ollama`, `nomic-embed-text`, `http://localhost:11434` |

Overriding one setting keeps the others: `--embedding-endpoint` alone
still uses the model from env or config. The environment outranks `-c`
for these settings.

```bash
ctxt find "onboarding" --embedding-model snowflake-arctic-embed2
CTXT_EMBEDDING_MODEL=snowflake-arctic-embed2 ctxt find "onboarding"
ctxt find "onboarding" -c providers.embedding.model=snowflake-arctic-embed2
```

### Use a remote Ollama for one run

Forward a local port to the remote machine's Ollama (port 11434), for
example with an SSH tunnel on local port 11555, then point the run at it:

```bash
ctxt find "onboarding" --embedding-endpoint http://127.0.0.1:11555
CTXT_EMBEDDING_ENDPOINT=http://127.0.0.1:11555 dpkms dev reindex-vectors
```

The remote Ollama must have the model pulled.

### Check what a command will use

`ctxt embeddings provider` prints the resolved settings and the layer each
came from. With the config above: It accepts the same flags, env and `-c` as the commands above,
and never prints a key value.

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
  }
}
```

Pass a registered `model_id` to see how a command targeting that model
resolves it, including the model's own registry settings:

```bash
ctxt embeddings provider ollama-snowflake-arctic-embed2@2026-09-26
```

`dpkms serve` prints the resolved provider at startup.

---

## Examples

### OpenAI Only
```bash
OPENAI_API_KEY=sk-...
OPENAI_MODEL=gpt-4-turbo-preview
OPENAI_MAX_TOKENS=4096
OPENAI_TEMPERATURE=0.7
```

### Anthropic Only
```bash
ANTHROPIC_API_KEY=sk-ant-...
ANTHROPIC_MODEL=claude-3-opus-20240229
ANTHROPIC_MAX_TOKENS=4096
```

### Local with Ollama
```bash
OLLAMA_ENABLED=true
OLLAMA_HOST=http://localhost:11434
OLLAMA_MODEL=mistral
```

### Multi-Provider (with fallback)
```bash
# Primary: OpenAI
OPENAI_API_KEY=sk-...
OPENAI_MODEL=gpt-4-turbo-preview

# Fallback: Anthropic
ANTHROPIC_API_KEY=sk-ant-...
ANTHROPIC_MODEL=claude-3-sonnet-20240229

# Local development: Ollama
OLLAMA_ENABLED=true
OLLAMA_MODEL=llama2
```

---

## Cost Optimization

**High volume, cost-conscious:**
```bash
OPENAI_MODEL=gpt-3.5-turbo  # Cheaper
OPENAI_MAX_TOKENS=2048      # Lower limits
```

**Quality-focused:**
```bash
OPENAI_MODEL=gpt-4-turbo-preview
OPENAI_MAX_TOKENS=8192
OPENAI_TEMPERATURE=0.3  # More deterministic
```

---

## Related

- [../ctxt/pipelines.md](../ctxt/pipelines.md) — Pipeline AI integration
- [../scaling.md](../scaling.md#cost-optimization) — Cost optimization

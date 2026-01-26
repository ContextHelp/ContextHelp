# AI Providers Environment Variables

Configuration for OpenAI, Anthropic, and Ollama.

---

## OpenAI

### OPENAI_API_KEY
**Type:** string (secret)  
**Default:** _(none)_  
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
**Type:** string  
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
**Type:** integer  
**Default:** `4096`

Maximum tokens per API call.

```bash
OPENAI_MAX_TOKENS=8192
```

### OPENAI_TEMPERATURE
**Type:** float (0.0-2.0)  
**Default:** `0.7`

Sampling temperature for generation.

```bash
OPENAI_TEMPERATURE=0.5
```

---

## Anthropic (Claude)

### ANTHROPIC_API_KEY
**Type:** string (secret)  
**Default:** _(none)_

Anthropic API key. **Never commit to version control.**

```bash
ANTHROPIC_API_KEY=sk-ant-...
```

### ANTHROPIC_MODEL
**Type:** string  
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
**Type:** integer  
**Default:** `4096`

Maximum tokens per API call.

```bash
ANTHROPIC_MAX_TOKENS=8192
```

---

## Ollama (Local Models)

### OLLAMA_ENABLED
**Type:** boolean  
**Default:** `false`

Enable Ollama for local AI models.

```bash
OLLAMA_ENABLED=true
```

### OLLAMA_HOST
**Type:** string  
**Default:** `http://localhost:11434`

Ollama server URL.

```bash
OLLAMA_HOST=http://ollama.internal:11434
```

### OLLAMA_MODEL
**Type:** string  
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

# LLM Provider Support Implementation Plan

## Overview

Add agnostic LLM and embedding provider support to ctxt to enable pre-retrieval intent routing and future AI-powered features.

## Stack Decision

| Component | Library | Rationale |
|-----------|---------|-----------|
| **LLM** | `charm.land/fantasy` | Charm-maintained, 8+ providers, idiomatic Go, streaming, tool calling |
| **Embeddings** | `github.com/milosgajdos/go-embeddings` | Multi-provider (Ollama, OpenAI, Bedrock, Cohere, Vertex, Voyage), includes document splitting |

## Supported Providers

### LLM (via Fantasy)
- OpenAI (GPT-4, GPT-4o, etc.)
- Anthropic (Claude)
- OpenRouter (200+ models)
- Ollama (local: llama3.2, mistral, etc.)
- LM Studio (local)
- Azure OpenAI
- AWS Bedrock
- Google Vertex AI / Gemini

### Embeddings (via go-embeddings)
- **Ollama** (local, free): nomic-embed-text, mxbai-embed-large, all-minilm
- **OpenAI**: text-embedding-3-small, text-embedding-3-large
- **Cohere**: embed-english-v3.0
- **Voyage**: voyage-2
- **AWS Bedrock**: titan-embed-text
- **Google Vertex**: textembedding-gecko

## Architecture

```
internal/llm/
├── llm.go              # Fantasy wrapper, chat operations
├── embed.go            # Embedding interface + factory
├── config.go           # Provider configuration types
└── llm_test.go         # Tests
```

## Configuration

Add to ctxt config (YAML):

```yaml
llm:
  provider: ollama                    # ollama | openai | anthropic | openrouter
  model: llama3.2
  base_url: http://localhost:11434    # For local providers
  api_key: ""                         # For cloud providers (or env var)

embedding:
  provider: ollama                    # ollama | openai | cohere | voyage | bedrock | vertex
  model: nomic-embed-text             # nomic-embed-text | mxbai-embed-large | text-embedding-3-small
  base_url: http://localhost:11434
  api_key: ""                         # For cloud providers
```

Environment variables:
- `LLM_PROVIDER` / `LLM_MODEL` / `LLM_BASE_URL` / `LLM_API_KEY`
- `EMBEDDING_PROVIDER` / `EMBEDDING_MODEL` / `EMBEDDING_BASE_URL` / `EMBEDDING_API_KEY`

## Implementation Phases

### Phase 1: Dependencies & Types

**Files:** `internal/llm/config.go`, `go.mod`

1. Add dependencies to `go.mod`:
   ```
   charm.land/fantasy v0.8.1
   github.com/milosgajdos/go-embeddings v0.5.0
   ```

2. Create config types:
   ```go
   type LLMConfig struct {
       Provider string `yaml:"provider" env:"LLM_PROVIDER"`
       Model    string `yaml:"model" env:"LLM_MODEL"`
       BaseURL  string `yaml:"base_url" env:"LLM_BASE_URL"`
       APIKey   string `yaml:"api_key" env:"LLM_API_KEY"`
   }
   
   type EmbeddingConfig struct {
       Provider string `yaml:"provider" env:"EMBEDDING_PROVIDER"`
       Model    string `yaml:"model" env:"EMBEDDING_MODEL"`
       BaseURL  string `yaml:"base_url" env:"EMBEDDING_BASE_URL"`
       APIKey   string `yaml:"api_key" env:"EMBEDDING_API_KEY"`
   }
   ```

### Phase 2: LLM Wrapper

**Files:** `internal/llm/llm.go`

1. Create Fantasy-based client wrapper
2. Implement chat and streaming methods
3. Provider factory function:
   ```go
   func NewClient(cfg LLMConfig) (*Client, error)
   ```

### Phase 3: Embedding Wrapper

**Files:** `internal/llm/embed.go`

1. Create unified embedding interface
2. Wrap go-embeddings providers
3. Factory function:
   ```go
   func NewEmbedder(cfg EmbeddingConfig) (Embedder, error)
   ```

### Phase 4: Service Integration

**Files:** `internal/service/service.go`

1. Add LLM and Embedder to Service struct
2. Initialize from config in `NewService()`
3. Add methods:
   ```go
   func (s *Service) Chat(ctx context.Context, prompt string) (string, error)
   func (s *Service) Embed(ctx context.Context, texts []string) ([][]float32, error)
   ```

### Phase 5: CLI Flags

**Files:** `cmd/ctxt/cmd/root.go` or new flags

Add flags:
```bash
--llm-provider    Override LLM provider
--llm-model       Override LLM model
--embed-provider  Override embedding provider
--embed-model     Override embedding model
```

## Interface Design

### LLM Client

```go
package llm

type Client struct {
    model   fantasy.LanguageModel
    provider string
    modelID  string
}

func NewClient(cfg LLMConfig) (*Client, error)

func (c *Client) Generate(ctx context.Context, prompt string) (string, error)
func (c *Client) GenerateWithSystem(ctx context.Context, system, prompt string) (string, error)
func (c *Client) Stream(ctx context.Context, prompt string) (<-chan StreamChunk, error)

type StreamChunk struct {
    Content string
    Done    bool
    Error   error
}
```

### Embedder

```go
package llm

type Embedder interface {
    Embed(ctx context.Context, texts []string) ([][]float32, error)
    Dimension() int
    Model() string
}

func NewEmbedder(cfg EmbeddingConfig) (Embedder, error)
```

## Default Configuration

For local-first, zero-config experience:

```yaml
llm:
  provider: ollama
  model: llama3.2
  base_url: http://localhost:11434

embedding:
  provider: ollama
  model: nomic-embed-text
  base_url: http://localhost:11434
```

## Testing Strategy

1. **Unit tests** with mock providers
2. **Integration tests** with Ollama (local, free)
3. **Optional cloud tests** (skip if no API key)

## File Changes Summary

| File | Action | Description |
|------|--------|-------------|
| `go.mod` | Modify | Add fantasy, go-embeddings deps |
| `internal/llm/config.go` | Create | Config types |
| `internal/llm/llm.go` | Create | Fantasy wrapper |
| `internal/llm/embed.go` | Create | Embedding wrapper |
| `internal/llm/llm_test.go` | Create | Tests |
| `internal/service/service.go` | Modify | Add LLM/Embedder fields |
| `cmd/ctxt/cmd/root.go` | Modify | Add CLI flags |
| `configs/default.yaml` | Modify | Add llm/embedding sections |

## Estimated Effort

| Phase | Time |
|-------|------|
| Phase 1: Dependencies & Types | 30 min |
| Phase 2: LLM Wrapper | 1-2 hours |
| Phase 3: Embedding Wrapper | 1 hour |
| Phase 4: Service Integration | 1 hour |
| Phase 5: CLI Flags | 30 min |
| Testing | 1 hour |
| **Total** | **5-6 hours** |

## Future: Pre-Retrieval Intent Routing

Once LLM support is in place, implement intent routing:

```go
func (s *Service) ShouldRetrieve(ctx context.Context, query string) (bool, string, error) {
    prompt := fmt.Sprintf(intentPrompt, query)
    response, err := s.llm.Generate(ctx, prompt)
    // Parse RETRIEVE/NO_RETRIEVE + rewritten query
}
```

## Success Criteria

- [ ] LLM chat works with Ollama (local)
- [ ] LLM chat works with OpenAI (cloud)
- [ ] Embeddings work with Ollama (local)
- [ ] Embeddings work with OpenAI (cloud)
- [ ] Configuration via YAML and env vars
- [ ] CLI flags for overrides
- [ ] Unit tests pass
- [ ] No breaking changes to existing functionality

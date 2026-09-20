---
status: shipped
---

# Story: Configure AI Provider

**System Types:** dpkms (self-hosted), dpkms cloud
**Personas:** [Maintainers](../../personas/maintainers.md), [Operations](../../personas/operations.md)

---

## User Goal

As an operator or maintainer, I want to configure and switch between AI providers for enrichment and NLQ normalization without downtime.

---

## Context

dPKMS supports multiple AI providers (OpenAI, Anthropic, LMQL, instructor, outlines) with different trade-offs. Operators need flexibility to choose providers based on cost, latency, capability, or compliance requirements. Configuration should be polymorphic and support runtime changes.

---

## Acceptance Criteria

- [ ] Operators can configure AI provider via environment variables
- [ ] Operators can configure AI provider via YAML config file
- [ ] Supported providers: openai, anthropic, ollama, lmql, instructor, outlines
- [ ] Provider configuration includes: type, model, endpoint, credentials, rate limits
- [ ] Configuration can be changed without service restart (hot-reload)
- [ ] Fallback provider is supported (e.g., LMQL with OpenAI fallback)
- [ ] All enrichment steps use configured provider automatically
- [ ] NLQ normalizer uses configured provider for intent classification
- [ ] Cost tracking: provider usage and cost metrics are monitored
- [ ] Health checks: verify provider connectivity and rate limits

---

## Implementation Notes

### Environment Variables

```bash
# OpenAI (default)
export CH_AI_PROVIDER_TYPE=openai
export CH_AI_OPENAI_MODEL=gpt-4o
export CH_AI_OPENAI_API_KEY=sk-...

# Anthropic
export CH_AI_PROVIDER_TYPE=anthropic
export CH_AI_ANTHROPIC_MODEL=claude-opus-4-1
export CH_AI_ANTHROPIC_API_KEY=sk-ant-...

# Local LMQL with fallback
export CH_AI_PROVIDER_TYPE=lmql
export CH_AI_LMQL_BACKEND=local
export CH_AI_LMQL_ENDPOINT=http://localhost:8080
export CH_AI_LMQL_MODEL=llama3
export CH_AI_LMQL_FALLBACK_TYPE=openai
export CH_AI_LMQL_FALLBACK_API_KEY=sk-...

# Instructor (API with Pydantic retry)
export CH_AI_PROVIDER_TYPE=instructor
export CH_AI_INSTRUCTOR_BASE_PROVIDER=openai
export CH_AI_INSTRUCTOR_BASE_MODEL=gpt-4
```

### YAML Configuration

```yaml
# config.yaml
aiProvider:
  type: openai
  model: gpt-4o
  apiKey: ${CH_OPENAI_API_KEY}
  baseURL: https://api.openai.com/v1  # optional
  rateLimit:
    requestsPerMinute: 100
    tokensPerMinute: 90000
  timeout: 30s
  retryPolicy:
    maxAttempts: 3
    backoffMultiplier: 2
    initialBackoffMs: 100

# Or with fallback
aiProvider:
  type: lmql
  backend: local
  model: llama3
  endpoint: http://localhost:8080
  fallback:
    type: openai
    model: gpt-4o
    apiKey: ${CH_OPENAI_API_KEY}
```

### Provider Registry

```go
type AIProviderFactory struct {
  providers map[string]AIProvider
}

func (f *AIProviderFactory) GetProvider(config ProviderConfig) (AIProvider, error) {
  switch config.Type {
  case "openai":
    return NewOpenAIProvider(config)
  case "anthropic":
    return NewAnthropicProvider(config)
  case "lmql":
    return NewLMQLProvider(config)
  case "instructor":
    return NewInstructorProvider(config)
  case "outlines":
    return NewOutlinesProvider(config)
  default:
    return nil, fmt.Errorf("unknown provider: %s", config.Type)
  }
}

// Usage in enrichment pipeline
func (step *EnrichmentStep) Execute(ctx ExecutionContext, input interface{}) (interface{}, error) {
  provider, _ := step.providerFactory.GetProvider(step.config.AIProvider)

  constraints := Constraints{
    OutputFormat: "json",
    Schema:       EntityListSchema,
  }

  return provider.Call(ctx, prompt, constraints)
}
```

### Hot Reload Configuration

```go
type ConfigManager struct {
  currentConfig ProviderConfig
  mu            sync.RWMutex
}

func (m *ConfigManager) ReloadConfig(newConfig ProviderConfig) error {
  // Validate new config
  provider, err := factory.GetProvider(newConfig)
  if err != nil {
    return err
  }

  // Health check: verify connectivity
  if err := provider.HealthCheck(context.Background()); err != nil {
    return fmt.Errorf("health check failed: %w", err)
  }

  // Atomic switch
  m.mu.Lock()
  m.currentConfig = newConfig
  m.mu.Unlock()

  return nil
}

func (m *ConfigManager) GetProvider() AIProvider {
  m.mu.RLock()
  defer m.mu.RUnlock()
  return m.currentConfig.Provider
}
```

### Cost Tracking

```go
type CostTracker struct {
  provider      string
  totalCalls    int64
  totalTokens   int64
  totalCostUSD  float64
}

func (c *CostTracker) RecordCall(tokens int, costUSD float64) {
  c.totalCalls++
  c.totalTokens += tokens
  c.totalCostUSD += costUSD

  // Emit metric
  ch_ai_cost_usd.Add(costUSD)
  ch_ai_tokens_used.Add(float64(tokens))
}

// Prometheus metric
// ch_ai_cost_usd{provider="openai",env="prod"}
// ch_ai_tokens_used{provider="openai",model="gpt-4",env="prod"}
```

---

## E2E Test Checklist

### Config via Environment Variables
- [ ] EnvVar: `CH_AI_PROVIDER_TYPE=openai` sets provider type in loaded config
- [ ] EnvVar: `CH_AI_OPENAI_MODEL` sets model field (verify `cfg.Providers.LLM.Model` contains value)
- [ ] EnvVar: `CH_AI_PROVIDER_TYPE=anthropic` + `CH_AI_ANTHROPIC_MODEL` → provider type + model stored in config
- [ ] EnvVar: `CH_AI_PROVIDER_TYPE=lmql` + `CH_AI_LMQL_ENDPOINT` → endpoint stored in config

### Config via YAML File
- [ ] YAML: `providers.llm.backend` field written to config file is read back correctly
- [ ] YAML: `providers.llm.model` field persists across process restarts
- [ ] YAML: `providers.llm.endpoint` field persists (Ollama/LMQL endpoint)
- [ ] YAML: Fallback provider config round-trips through YAML marshal/unmarshal without data loss

### Provider Selection (Server-Side Receipt)
- [ ] Server: `dpkms serve` reads `providers.llm.backend` from config and initialises correct factory backend
- [ ] Server: `dpkms serve --profile <name>` sets `profile.default` via viper; server uses that profile
- [ ] Server: POST `/api/v1/pipelines/enqueue` with `{"type":"text","pipeline":"text.short"}` → server records
  `job.Pipeline = "text.short"` in storage (verify via GET `/api/v1/jobs/{id}`)
- [ ] Server: `GET /health` returns `{"status":"ok"}` when provider backend is reachable

### Per-Pipeline Provider Overrides
- [ ] Override: YAML `pipelines.overrides.<name>.providers.llm.backend` loaded into `PipelinesConfig`
- [ ] Override: Jobs routed to overridden pipeline use the overridden provider (verify via job metadata)

### Error / Fallback
- [ ] Error: Invalid `CH_AI_PROVIDER_TYPE` value → config.Load returns validation error (no silent default)
- [ ] Fallback: When primary LLM backend is unavailable, fallback backend field in config is non-empty

### Retry / Rate Limit
- [ ] Retry: `retryPolicy.maxAttempts` field survives YAML round-trip
- [ ] RateLimit: `rateLimit.requestsPerMinute` field survives YAML round-trip

---

## Related Stories

- [US-0014](../enrichment/US-0014-constrain-extraction-with-lmql.md) — LMQL-specific configuration
- [US-0015](../enrichment/US-0015-batch-enrichment-with-progress.md) — Provider used at scale
- [US-0016](../search/US-0016-natural-language-search.md) — NLQ normalizer uses provider
- [US-0032](../operations/US-0032-monitor-job-queue-health.md) — Provider metrics monitoring

---

## Personas

- [Maintainers](../../personas/maintainers.md)
- Platform Engineer
- [Operations](../../personas/operations.md)

---

## E2E Tests

- `test/integration/us0027_ai_provider_test.go::TestUS0027_EnvBackendLLMStub`
- `test/integration/us0027_ai_provider_test.go::TestUS0027_ProviderCalledAndResultStored`
- `test/integration/us0027_ai_provider_test.go::TestUS0027_ConfigRoundTripLLMBackend`
- `test/integration/us0027_ai_provider_test.go::TestUS0027_FactoryUsesResolverNotEnv`
- `test/integration/us0027_ai_provider_test.go::TestUS0027_NilResolverFallsBackToEnv`
- `test/integration/us0027_ai_provider_test.go::TestUS0027_EnvResolverReadOnly`
- `test/integration/us0027_ai_provider_test.go::TestUS0027_AnalyzeEndpointUsesConfiguredPipeline`

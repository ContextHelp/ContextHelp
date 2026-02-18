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

### REST API: Configuration Endpoints

```
GET /admin/config/ai-provider
→ 200 OK
{
  "type": "openai",
  "model": "gpt-4o",
  "provider_health": "healthy",
  "last_health_check": "2025-01-18T10:30:45Z"
}

PUT /admin/config/ai-provider
Content-Type: application/json

{
  "type": "lmql",
  "backend": "local",
  "model": "llama3",
  "endpoint": "http://localhost:8080"
}

→ 200 OK
{
  "status": "reconfigured",
  "new_config": {...},
  "applied_at": "2025-01-18T10:30:46Z"
}
```

### Health Check Endpoint

```
GET /admin/health/ai-provider
→ 200 OK
{
  "provider": "openai",
  "status": "healthy",
  "rate_limit": {
    "requests_per_minute": 100,
    "current_usage": 42,
    "remaining": 58
  },
  "latency_p99_ms": 1234,
  "error_rate": 0.001,
  "last_error": null
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

- [ ] Config: OpenAI provider configured via env vars
- [ ] Config: OpenAI provider configured via YAML
- [ ] Config: Anthropic provider configured
- [ ] Config: LMQL provider with fallback configured
- [ ] Provider: Enrichment step uses configured provider
- [ ] Provider: NLQ normalizer uses configured provider
- [ ] HotReload: Change provider config without restart
- [ ] HotReload: Active enrichment jobs continue to completion
- [ ] HotReload: New jobs use new provider configuration
- [ ] Health: Health check endpoint returns provider status
- [ ] Health: Health check detects rate limit exceeded
- [ ] Health: Health check detects provider down (returns unhealthy)
- [ ] Fallback: LMQL provider falls back to OpenAI on error
- [ ] Cost: Cost tracker records tokens and USD cost
- [ ] Metrics: Prometheus metrics available for cost and usage
- [ ] Retry: Provider retry policy applies on transient failures

---

## Related Stories

- [constrain-extraction-with-lmql](../enrichment/constrain-extraction-with-lmql.md) — LMQL-specific configuration
- [batch-enrichment-with-progress](../enrichment/batch-enrichment-with-progress.md) — Provider used at scale
- [natural-language-search](../search/natural-language-search.md) — NLQ normalizer uses provider
- [monitor-job-queue-health](../operations/monitor-job-queue-health.md) — Provider metrics monitoring

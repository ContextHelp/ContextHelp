# BAML Provider Implementation Guide (Go)

**Status:** Draft
**Sprint:** 001 (Reference Implementation)
**Related:** `ENRICHMENT-PROVIDER-CONTRACT.md`, `docs/ctxt/pipelines.md`

---

## Overview

This document describes the reference implementation of the Enrichment Provider Contract using BAML as the extraction engine, implemented in **Go**.

The BAML provider serves as:
1. **Reference implementation** — validates the contract design
2. **Production provider** — high-quality structured extraction
3. **Integration example** — shows how to implement the contract

---

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│ ctxt Pipeline Engine (Go)                                   │
└──────────────────────┬──────────────────────────────────────┘
                       ↓
┌─────────────────────────────────────────────────────────────┐
│ BamlEnrichmentProvider (Go)                                 │
│ • Implements EnrichmentProvider interface                   │
│ • Calls BAML CLI or Python runtime                          │
│ • Converts between ctxt and BAML types                      │
└──────────────────────┬──────────────────────────────────────┘
                       ↓
┌─────────────────────────────────────────────────────────────┐
│ BAML Runtime (CLI/Python)                                   │
│ • Schema validation                                         │
│ • Type-safe LLM calls                                       │
│ • Multi-provider support (Anthropic, OpenAI, etc)           │
│ • Retry logic and error handling                            │
└──────────────────────┬──────────────────────────────────────┘
                       ↓
┌─────────────────────────────────────────────────────────────┐
│ LLM Provider API (Anthropic, OpenAI, etc)                   │
└─────────────────────────────────────────────────────────────┘
```

**Note:** BAML doesn't have a native Go runtime yet, so we'll use one of these approaches:
1. **Call BAML CLI** as subprocess (simplest)
2. **Use BAML Python runtime** via cgo or gRPC bridge
3. **Wait for Go runtime** (future)
4. **Implement direct LLM provider** instead (no BAML dependency)

For now, we'll design for **approach #1 (CLI)** as it's the most straightforward.

---

## Implementation

### Project Structure

```
ctxt/
├─ schemas/
│  └─ knowledge_object.baml          # BAML schema (already created)
├─ pkg/
│  ├─ enrichment/
│  │  ├─ contract.go                 # Interface definition (dPKMS)
│  │  ├─ types.go                    # Common types
│  │  ├─ errors.go                   # Error types
│  │  ├─ registry.go                 # Provider registry
│  │  └─ providers/
│  │     ├─ baml.go                  # BAML CLI provider
│  │     ├─ direct.go                # Direct API provider
│  │     └─ hybrid.go                # Hybrid provider
│  └─ ...
├─ cmd/
│  ├─ ctxt/
│  └─ dpkms/
└─ go.mod
```

### Go Module Dependencies

```go
// go.mod additions
require (
    // LLM client libraries
    github.com/anthropics/anthropic-sdk-go v0.x
    github.com/sashabaranov/go-openai v1.x

    // Existing dependencies
    github.com/spf13/cobra v1.10.2
    github.com/spf13/viper v1.21.0
)
```

### Contract Definition

```go
// pkg/enrichment/contract.go

package enrichment

import (
    "context"
    "time"
)

// EnrichmentProvider is the core interface for all enrichment providers
type EnrichmentProvider interface {
    // ID returns the provider identifier (e.g., "baml-0.1.0", "direct-anthropic")
    ID() string

    // Enrich extracts structured knowledge from unstructured content
    Enrich(ctx context.Context, req *EnrichmentRequest) (*KnowledgeObject, error)

    // Capabilities describes what this provider can do
    Capabilities() ProviderCapabilities

    // EstimateCost estimates cost before execution (for transparency)
    EstimateCost(req *EnrichmentRequest) Cost

    // EnrichBatch processes multiple requests (optional optimization)
    EnrichBatch(ctx context.Context, reqs []*EnrichmentRequest) ([]*KnowledgeObject, error)
}

}

// EnrichmentRequest represents input to enrichment
type EnrichmentRequest struct {
    Content          UnstructuredContent
    TargetSchema     SchemaVersion
    Context          EnrichmentContext
    Profile          FocusProfile
    RegistryHints    []RegistryHint
    PriorExtraction  *KnowledgeObject // For re-enrichment
}

// UnstructuredContent is raw input
type UnstructuredContent struct {
    Format   ContentFormat
    Data     ContentData
    Metadata ContentMetadata
}

type ContentFormat string

const (
    FormatPlainText ContentFormat = "plain_text"
    FormatMarkdown  ContentFormat = "markdown"
    FormatHTML      ContentFormat = "html"
    FormatPDF       ContentFormat = "pdf"
    FormatImage     ContentFormat = "image"
    FormatAudio     ContentFormat = "audio"
    FormatVideo     ContentFormat = "video"
)

type ContentData struct {
    Text   *string // For text-based content
    Binary []byte  // For binary content
    URL    *string // For URL-based content
}

type ContentMetadata struct {
    SourceURL   string
    Title       string
    Author      string
    PublishedAt *time.Time
    Language    string
    SizeBytes   int
}

// EnrichmentContext provides execution constraints
type EnrichmentContext struct {
    UserPreferences    Preferences
    BudgetConstraints  *BudgetLimit
    QualityThreshold   float64
    MaxLatency         *time.Duration
    PrivacyMode        bool
    IsReEnrichment     bool
}

type BudgetLimit struct {
    MaxCostUSD float64
    MaxTokens  *int
}

// KnowledgeObject is the structured output
type KnowledgeObject struct {
    Summary      string
    AtomicNotes  []AtomicNote
    Sections     []Section
    Tags         []Tag
    Mentions     []Mention
    Entities     []Entity
    Decisions    []Decision
    Tasks        []Task
    Questions    []Question
    Metadata     ObjectMetadata
    Provenance   EnrichmentProvenance
    Confidence   ConfidenceScores
}

type AtomicNote struct {
    Content        string
    SourceLocation *string
    Confidence     float64
    Tags           []string
}

type DecisionImpact string

const (
    ImpactLow    DecisionImpact = "LOW"
    ImpactMedium DecisionImpact = "MEDIUM"
    ImpactHigh   DecisionImpact = "HIGH"
)

type DecisionStatus string

const (
    StatusOpen       DecisionStatus = "OPEN"
    StatusResolved   DecisionStatus = "RESOLVED"
    StatusSuperseded DecisionStatus = "SUPERSEDED"
)

type Decision struct {
    WhatWasDecided         string
    Rationale              *string
    Impact                 DecisionImpact
    Status                 DecisionStatus
    Stakeholders           []string
    Timestamp              *time.Time
    Confidence             float64
    AlternativesConsidered []string
}

type Task struct {
    Description  string
    Priority     *string
    Assignee     *string
    DueDate      *time.Time
    Confidence   float64
    Dependencies []string
    Context      *string
}

type Mention struct {
    Text          string
    MentionType   MentionType
    Namespace     string
    Slug          string
    Confidence    float64
    CanonicalName *string
}

type MentionType string

const (
    MentionEntity       MentionType = "entity"
    MentionPerson       MentionType = "person"
    MentionConcept      MentionType = "concept"
    MentionLocation     MentionType = "location"
    MentionProduct      MentionType = "product"
    MentionOrganization MentionType = "organization"
    MentionEvent        MentionType = "event"
    MentionProject      MentionType = "project"
    MentionSystem       MentionType = "system"
)

type EnrichmentProvenance struct {
    EnrichedBy          string
    ProviderVersion     string
    Model               *string
    EnrichedAt          time.Time
    SchemaVersion       SchemaVersion
    Cost                *Cost
    Tokens              *TokenUsage
    Latency             time.Duration
    CompatibleProviders []string
}

type Cost struct {
    USD    float64
    Tokens *TokenUsage
}

type TokenUsage struct {
    InputTokens  int
    OutputTokens int
}

type ProviderCapabilities struct {
    SupportsMultimodal bool
    MaxContentSize     int
    SupportedFormats   []ContentFormat
    RequiresNetwork    bool
    SupportsStreaming  bool
    SupportsBatching   bool
    CostModel          CostModel
    QualityProfile     QualityProfile
}

type CostModel struct {
    Type           string  // "free", "per_token", "per_request", "hybrid"
    InputCostUSD   float64 // Per token
    OutputCostUSD  float64 // Per token
    BaseCostUSD    float64 // Per request
}

type QualityProfile struct {
    TypicalQuality  float64
    TypicalLatency  time.Duration
    Reliability     float64 // Uptime/success rate
}

type ConfidenceScores struct {
    Overall          float64
    Summary          float64
    Extractions      float64
    EntityResolution float64
}
```

---

## BAML Provider Implementation

```go
// pkg/enrichment/providers/baml.go

package providers

import (
    "context"
    "encoding/json"
    "fmt"
    "os"
    "os/exec"
    "path/filepath"
    "time"

    "github.com/ideacrafterslabs/ctxt/pkg/enrichment"
)

// BamlProvider implements EnrichmentProvider using BAML CLI
type BamlProvider struct {
    config BamlConfig
}

type BamlConfig struct {
    SchemaPath     string
    DefaultClient  string
    FallbackClient string
    MaxRetries     int
    BamlCLIPath    string // Path to baml executable
}

// NewBamlProvider creates a new BAML provider
func NewBamlProvider(config BamlConfig) (*BamlProvider, error) {
    // Validate schema file exists
    if _, err := os.Stat(config.SchemaPath); err != nil {
        return nil, fmt.Errorf("BAML schema not found: %w", err)
    }

    // Find BAML CLI
    if config.BamlCLIPath == "" {
        path, err := exec.LookPath("baml")
        if err != nil {
            return nil, fmt.Errorf("BAML CLI not found in PATH: %w", err)
        }
        config.BamlCLIPath = path
    }

    return &BamlProvider{config: config}, nil
}

func (p *BamlProvider) ID() string {
    return "baml-0.1.0"
}

func (p *BamlProvider) Enrich(ctx context.Context, req *enrichment.EnrichmentRequest) (*enrichment.KnowledgeObject, error) {
    start := time.Now()

    // Check content size limits
    caps := p.Capabilities()
    if req.Content.Metadata.SizeBytes > caps.MaxContentSize {
        return nil, &enrichment.ContentTooLargeError{
            Size: req.Content.Metadata.SizeBytes,
            Max:  caps.MaxContentSize,
        }
    }

    // Estimate cost and check budget
    estimatedCost := p.EstimateCost(req)
    if req.Context.BudgetConstraints != nil {
        if estimatedCost.USD > req.Context.BudgetConstraints.MaxCostUSD {
            return nil, &enrichment.BudgetExceededError{
                Spent: estimatedCost.USD,
                Limit: req.Context.BudgetConstraints.MaxCostUSD,
            }
        }
    }

    // Prepare BAML input
    bamlInput, err := p.prepareBamlInput(req)
    if err != nil {
        return nil, fmt.Errorf("prepare input: %w", err)
    }

    // Call BAML with retries
    var lastErr error
    for attempt := 0; attempt <= p.config.MaxRetries; attempt++ {
        obj, err := p.callBamlCLI(ctx, bamlInput, req)
        if err == nil {
            latency := time.Since(start)

            // Check quality threshold
            if obj.Confidence.Overall < req.Context.QualityThreshold {
                return nil, &enrichment.QualityTooLowError{
                    Quality:   obj.Confidence.Overall,
                    Threshold: req.Context.QualityThreshold,
                }
            }

            // Add provenance
            obj.Provenance = enrichment.EnrichmentProvenance{
                EnrichedBy:      p.ID(),
                ProviderVersion: "0.1.0",
                Model:           &p.config.DefaultClient,
                EnrichedAt:      time.Now(),
                SchemaVersion:   req.TargetSchema,
                Cost:            &estimatedCost,
                Latency:         latency,
                CompatibleProviders: []string{
                    "baml-0.2.0",
                    "direct-anthropic-1.0",
                },
            }

            return obj, nil
        }

        lastErr = err
        if attempt < p.config.MaxRetries {
            // Exponential backoff
            backoff := time.Duration(100*(1<<attempt)) * time.Millisecond
            time.Sleep(backoff)
        }
    }

    return nil, fmt.Errorf("enrichment failed after %d retries: %w", p.config.MaxRetries, lastErr)
}

func (p *BamlProvider) Capabilities() enrichment.ProviderCapabilities {
    return enrichment.ProviderCapabilities{
        SupportsMultimodal: true,
        MaxContentSize:     1_000_000, // 1MB
        SupportedFormats: []enrichment.ContentFormat{
            enrichment.FormatPlainText,
            enrichment.FormatMarkdown,
            enrichment.FormatHTML,
        },
        RequiresNetwork:   true,
        SupportsStreaming: false,
        SupportsBatching:  true,
        CostModel: enrichment.CostModel{
            Type:          "per_token",
            InputCostUSD:  0.000003, // Claude Sonnet pricing
            OutputCostUSD: 0.000015,
        },
        QualityProfile: enrichment.QualityProfile{
            TypicalQuality: 0.85,
            TypicalLatency: 5 * time.Second,
            Reliability:    0.99,
        },
    }
}

func (p *BamlProvider) EstimateCost(req *enrichment.EnrichmentRequest) enrichment.Cost {
    // Rough estimation: 1 char ≈ 0.25 tokens
    estimatedInputTokens := req.Content.Metadata.SizeBytes / 4

    // Structured output is typically 1000-3000 tokens
    var estimatedOutputTokens int
    switch {
    case req.Content.Metadata.SizeBytes <= 10_000:
        estimatedOutputTokens = 1_000
    case req.Content.Metadata.SizeBytes <= 50_000:
        estimatedOutputTokens = 2_000
    default:
        estimatedOutputTokens = 3_000
    }

    totalCost := float64(estimatedInputTokens)*0.000003 +
        float64(estimatedOutputTokens)*0.000015

    return enrichment.Cost{
        USD: totalCost,
        Tokens: &enrichment.TokenUsage{
            InputTokens:  estimatedInputTokens,
            OutputTokens: estimatedOutputTokens,
        },
    }
}

func (p *BamlProvider) EnrichBatch(ctx context.Context, reqs []*enrichment.EnrichmentRequest) ([]*enrichment.KnowledgeObject, error) {
    // Process in parallel using goroutines
    type result struct {
        obj *enrichment.KnowledgeObject
        err error
        idx int
    }

    resultsChan := make(chan result, len(reqs))

    for i, req := range reqs {
        go func(idx int, request *enrichment.EnrichmentRequest) {
            obj, err := p.Enrich(ctx, request)
            resultsChan <- result{obj: obj, err: err, idx: idx}
        }(i, req)
    }

    // Collect results in order
    results := make([]*enrichment.KnowledgeObject, len(reqs))
    for i := 0; i < len(reqs); i++ {
        res := <-resultsChan
        if res.err != nil {
            return nil, fmt.Errorf("batch item %d failed: %w", res.idx, res.err)
        }
        results[res.idx] = res.obj
    }

    return results, nil
}

// prepareBamlInput converts EnrichmentRequest to BAML CLI input
func (p *BamlProvider) prepareBamlInput(req *enrichment.EnrichmentRequest) (*bamlInput, error) {
        // Extract content text
        let content = match &request.content.data {
            ContentData::Text(text) => text.clone(),
            ContentData::Url(url) => {
                return Err(EnrichmentError::UnsupportedFormat(
                    ContentFormat::Url { url: url.clone() }
                ));
            }
            ContentData::Binary(_) => {
                return Err(EnrichmentError::UnsupportedFormat(
                    request.content.format.clone()
                ));
            }
        };

        // Prepare registry hints
        let registry_hints: Vec<String> = request.registry_hints
            .iter()
            .map(|hint| format!("{}", hint))
            .collect();

        Ok(BamlFunctionInput {
            content,
            content_type: format!("{:?}", request.content.format),
            profile: request.profile.name.clone(),
            registry_hints,
        })
    }

    /// Convert BAML output to KnowledgeObject
    fn parse_baml_output(
        &self,
        baml_output: BamlKnowledgeObject,
        request: &EnrichmentRequest,
        cost: Option<Cost>,
        latency: Duration,
    ) -> Result<KnowledgeObject, EnrichmentError> {
        // BAML's type system ensures this output already matches our schema
        // Just need to add provenance metadata

        let mut knowledge_object: KnowledgeObject = baml_output.into();

        knowledge_object.provenance = EnrichmentProvenance {
            enriched_by: self.id().to_string(),
            provider_version: env!("CARGO_PKG_VERSION").to_string(),
            model: Some(self.config.default_client.clone()),
            enriched_at: chrono::Utc::now(),
            schema_version: SchemaVersion::V1,
            cost,
            tokens: None, // BAML doesn't expose token counts yet
            latency,
            compatible_providers: vec![
                "baml-0.2.0".to_string(),
                "direct-anthropic-1.0".to_string(),
            ],
        };

        Ok(knowledge_object)
    }
}

#[async_trait]
impl EnrichmentProvider for BamlEnrichmentProvider {
    fn id(&self) -> &str {
        "baml-0.1.0"
    }

    async fn enrich(
        &self,
        request: EnrichmentRequest,
    ) -> Result<KnowledgeObject, EnrichmentError> {
        let start = Instant::now();

        // Check content size limits
        let caps = self.capabilities();
        if request.content.metadata.size_bytes > caps.max_content_size {
            return Err(EnrichmentError::ContentTooLarge {
                size: request.content.metadata.size_bytes,
                max: caps.max_content_size,
            });
        }

        // Estimate cost and check budget
        let estimated_cost = self.estimate_cost(&request);
        if let Some(budget) = &request.context.budget_constraints {
            if estimated_cost.usd > budget.max_cost_usd {
                return Err(EnrichmentError::BudgetExceeded {
                    spent: estimated_cost.usd,
                    limit: budget.max_cost_usd,
                });
            }
        }

        // Prepare BAML input
        let baml_input = self.prepare_baml_input(&request)?;

        // Call BAML function with retries
        let mut last_error = None;
        for attempt in 0..=self.config.max_retries {
            match self.call_baml_function(&baml_input, &request).await {
                Ok(baml_output) => {
                    let latency = start.elapsed();

                    // Check quality threshold
                    if baml_output.confidence.overall < request.context.quality_threshold {
                        return Err(EnrichmentError::QualityTooLow {
                            quality: baml_output.confidence.overall,
                            threshold: request.context.quality_threshold,
                        });
                    }

                    // Convert to KnowledgeObject
                    return self.parse_baml_output(
                        baml_output,
                        &request,
                        Some(estimated_cost),
                        latency,
                    );
                }
                Err(e) => {
                    last_error = Some(e);
                    if attempt < self.config.max_retries {
                        // Exponential backoff
                        tokio::time::sleep(Duration::from_millis(
                            100 * 2_u64.pow(attempt)
                        )).await;
                    }
                }
            }
        }

        Err(last_error.unwrap_or_else(||
            EnrichmentError::ProviderError("Unknown error".to_string())
        ))
    }

    fn capabilities(&self) -> ProviderCapabilities {
        ProviderCapabilities {
            supports_multimodal: true,
            max_content_size: 1_000_000, // 1MB (conservative)
            supported_formats: vec![
                ContentFormat::PlainText,
                ContentFormat::Markdown,
                ContentFormat::HTML,
            ],
            requires_network: true,
            supports_streaming: false, // BAML doesn't support streaming yet
            supports_batching: true,
            cost_model: CostModel::PerToken {
                input_cost: 0.000003,  // Claude Sonnet pricing
                output_cost: 0.000015,
            },
            quality_profile: QualityProfile {
                typical_quality: 0.85,
                typical_latency: Duration::from_secs(5),
                reliability: 0.99,
            },
        }
    }

    fn estimate_cost(&self, request: &EnrichmentRequest) -> Cost {
        // Rough estimation: 1 char ≈ 0.25 tokens (English text)
        let estimated_input_tokens = request.content.metadata.size_bytes / 4;

        // Structured output is typically 1000-3000 tokens depending on content
        let estimated_output_tokens = match request.content.metadata.size_bytes {
            0..=10_000 => 1_000,      // Small content
            10_001..=50_000 => 2_000, // Medium content
            _ => 3_000,               // Large content
        };

        let total_cost =
            (estimated_input_tokens as f64 * 0.000003) +
            (estimated_output_tokens as f64 * 0.000015);

        Cost {
            usd: total_cost,
            tokens: Some(TokenUsage {
                input_tokens: estimated_input_tokens,
                output_tokens: estimated_output_tokens,
            }),
        }
    }

    async fn enrich_batch(
        &self,
        requests: Vec<EnrichmentRequest>,
    ) -> Result<Vec<KnowledgeObject>, EnrichmentError> {
        // BAML supports concurrent calls, so batch by running in parallel
        let mut tasks = Vec::new();

        for request in requests {
            let provider = self.clone(); // Cheap clone (Arc internally)
            tasks.push(tokio::spawn(async move {
                provider.enrich(request).await
            }));
        }

        let mut results = Vec::new();
        for task in tasks {
            results.push(task.await.map_err(|e|
                EnrichmentError::ProviderError(format!("Task failed: {}", e))
            )??);
        }

        Ok(results)
    }
}

impl BamlEnrichmentProvider {
    /// Internal: Call BAML function
    async fn call_baml_function(
        &self,
        input: &BamlFunctionInput,
        request: &EnrichmentRequest,
    ) -> Result<BamlKnowledgeObject, EnrichmentError> {
        // Determine which BAML function to call
        let function_name = match &request.content.format {
            ContentFormat::PlainText | ContentFormat::Markdown | ContentFormat::HTML => {
                "EnrichContent"
            }
            ContentFormat::Image { .. } |
            ContentFormat::Audio { .. } |
            ContentFormat::Video { .. } => {
                "EnrichMultimodal"
            }
            _ => {
                return Err(EnrichmentError::UnsupportedFormat(
                    request.content.format.clone()
                ));
            }
        };

        // Call BAML function
        let result = self.runtime
            .call_function(
                function_name,
                &input,
                Some(&self.config.default_client),
            )
            .await
            .map_err(|e| self.handle_baml_error(e))?;

        // Parse result into KnowledgeObject
        let knowledge_object: BamlKnowledgeObject = result
            .parse()
            .map_err(|e| EnrichmentError::SchemaValidation(
                format!("Failed to parse BAML output: {}", e)
            ))?;

        Ok(knowledge_object)
    }

    /// Handle BAML-specific errors
    fn handle_baml_error(&self, error: RuntimeError) -> EnrichmentError {
        match error {
            RuntimeError::NetworkError(e) =>
                EnrichmentError::NetworkError(e),
            RuntimeError::RateLimitExceeded =>
                EnrichmentError::ProviderError("Rate limit exceeded".to_string()),
            RuntimeError::Timeout =>
                EnrichmentError::Timeout(Duration::from_secs(30)),
            _ =>
                EnrichmentError::ProviderError(error.to_string()),
        }
    }
}

// BAML function input type (matches BAML schema)
#[derive(Debug, Clone, serde::Serialize)]
struct BamlFunctionInput {
    content: String,
    content_type: String,
    profile: String,
    registry_hints: Vec<String>,
}

// BAML output type (matches BAML schema)
// This would be auto-generated by BAML, but shown here for clarity
#[derive(Debug, Clone, serde::Deserialize)]
struct BamlKnowledgeObject {
    summary: String,
    atomic_notes: Vec<BamlAtomicNote>,
    sections: Vec<BamlSection>,
    tags: Vec<BamlTag>,
    mentions: Vec<BamlMention>,
    entities: Vec<BamlEntity>,
    decisions: Vec<BamlDecision>,
    tasks: Vec<BamlTask>,
    questions: Vec<BamlQuestion>,
    confidence: BamlConfidenceScores,
}

// ... other BAML types (AtomicNote, Decision, etc.)
// These would be auto-generated by BAML's code generation

// Conversion from BAML types to ctxt types
impl From<BamlKnowledgeObject> for KnowledgeObject {
    fn from(baml: BamlKnowledgeObject) -> Self {
        KnowledgeObject {
            summary: baml.summary,
            atomic_notes: baml.atomic_notes.into_iter().map(Into::into).collect(),
            sections: baml.sections.into_iter().map(Into::into).collect(),
            tags: baml.tags.into_iter().map(Into::into).collect(),
            mentions: baml.mentions.into_iter().map(Into::into).collect(),
            entities: baml.entities.into_iter().map(Into::into).collect(),
            decisions: baml.decisions.into_iter().map(Into::into).collect(),
            tasks: baml.tasks.into_iter().map(Into::into).collect(),
            questions: baml.questions.into_iter().map(Into::into).collect(),
            confidence: baml.confidence.into(),
            metadata: Default::default(),
            provenance: Default::default(), // Set by caller
        }
    }
}

// Clone implementation for BamlEnrichmentProvider
// BAML runtime should be wrapped in Arc for cheap cloning
impl Clone for BamlEnrichmentProvider {
    fn clone(&self) -> Self {
        Self {
            runtime: self.runtime.clone(), // Arc clone
            config: self.config.clone(),
        }
    }
}
```

---

## Configuration

### User Configuration

Users configure the BAML provider in `~/.config/contexthelp/config.toml`:

```toml
[enrichment]
default_provider = "baml"

[enrichment.providers.baml]
type = "baml"
schema_path = "~/.config/contexthelp/schemas/knowledge_object.baml"
default_client = "anthropic"
fallback_client = "openai"
max_retries = 3

[enrichment.providers.baml.clients.anthropic]
api_key_env = "ANTHROPIC_API_KEY"
model = "claude-sonnet-4"

[enrichment.providers.baml.clients.openai]
api_key_env = "OPENAI_API_KEY"
model = "gpt-4-turbo"
```

### BAML Client Configuration

BAML clients are configured in a separate `baml_src/clients.baml` file:

```baml
client<llm> Anthropic {
  provider anthropic
  options {
    model "claude-sonnet-4"
    api_key env.ANTHROPIC_API_KEY
    max_tokens 4096
  }
}

client<llm> OpenAI {
  provider openai
  options {
    model "gpt-4-turbo"
    api_key env.OPENAI_API_KEY
  }
}

client<llm> Ollama {
  provider ollama
  options {
    model "llama3.2"
    base_url "http://localhost:11434"
  }
}
```

---

## Testing

### Unit Tests

```rust
#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    async fn test_baml_provider_contract_compliance() {
        let config = BamlConfig {
            schema_path: "schemas/knowledge_object.baml".to_string(),
            default_client: "anthropic".to_string(),
            fallback_client: None,
            max_retries: 1,
        };

        let provider = BamlEnrichmentProvider::new(config).unwrap();

        // Run standard contract compliance tests
        contexthelp_testing::enrichment::assert_provider_implements_contract(&provider).await;
    }

    #[tokio::test]
    async fn test_simple_enrichment() {
        let provider = create_test_provider();

        let request = EnrichmentRequest {
            content: UnstructuredContent {
                format: ContentFormat::PlainText,
                data: ContentData::Text(
                    "Anthropic released Claude 3.5 Sonnet today.".to_string()
                ),
                metadata: ContentMetadata {
                    size_bytes: 43,
                    ..Default::default()
                },
            },
            target_schema: SchemaVersion::V1,
            context: EnrichmentContext::default(),
            profile: FocusProfile { name: "Engineer".to_string() },
            registry_hints: vec![],
            prior_extraction: None,
        };

        let result = provider.enrich(request).await.unwrap();

        assert!(!result.summary.is_empty());
        assert!(result.confidence.overall > 0.0);
        assert_eq!(result.provenance.enriched_by, "baml-0.1.0");
    }

    #[tokio::test]
    async fn test_budget_enforcement() {
        let provider = create_test_provider();

        let request = EnrichmentRequest {
            content: large_test_content(),
            context: EnrichmentContext {
                budget_constraints: Some(BudgetLimit {
                    max_cost_usd: 0.001, // Very low budget
                    max_tokens: None,
                }),
                ..Default::default()
            },
            ..Default::default()
        };

        let result = provider.enrich(request).await;

        assert!(matches!(result, Err(EnrichmentError::BudgetExceeded { .. })));
    }

    #[tokio::test]
    async fn test_quality_threshold() {
        let provider = create_test_provider();

        let request = EnrichmentRequest {
            content: ambiguous_test_content(),
            context: EnrichmentContext {
                quality_threshold: 0.95, // Very high threshold
                ..Default::default()
            },
            ..Default::default()
        };

        let result = provider.enrich(request).await;

        // Should either succeed with high quality or fail threshold check
        match result {
            Ok(obj) => assert!(obj.confidence.overall >= 0.95),
            Err(EnrichmentError::QualityTooLow { .. }) => {
                // Expected for ambiguous content
            }
            Err(e) => panic!("Unexpected error: {}", e),
        }
    }
}
```

### Integration Tests

```rust
#[tokio::test]
#[ignore] // Requires API key
async fn test_real_enrichment_anthropic() {
    let provider = BamlEnrichmentProvider::new(production_config()).unwrap();

    let test_article = include_str!("../test_data/sample_article.txt");
    let request = create_request(test_article);

    let result = provider.enrich(request).await.unwrap();

    // Validate output quality
    assert!(result.summary.len() > 50);
    assert!(result.atomic_notes.len() > 0);
    assert!(result.confidence.overall > 0.7);

    // Validate provenance
    assert_eq!(result.provenance.model.as_ref().unwrap(), "claude-sonnet-4");
    assert!(result.provenance.cost.is_some());
}
```

---

## Performance Optimization

### Caching

```rust
use std::sync::Arc;
use tokio::sync::RwLock;
use std::collections::HashMap;

pub struct CachedBamlProvider {
    inner: BamlEnrichmentProvider,
    cache: Arc<RwLock<HashMap<String, KnowledgeObject>>>,
}

impl CachedBamlProvider {
    async fn enrich_with_cache(
        &self,
        request: EnrichmentRequest,
    ) -> Result<KnowledgeObject, EnrichmentError> {
        // Generate cache key
        let cache_key = self.cache_key(&request);

        // Check cache
        {
            let cache = self.cache.read().await;
            if let Some(cached) = cache.get(&cache_key) {
                return Ok(cached.clone());
            }
        }

        // Cache miss - enrich
        let result = self.inner.enrich(request).await?;

        // Store in cache
        {
            let mut cache = self.cache.write().await;
            cache.insert(cache_key, result.clone());
        }

        Ok(result)
    }

    fn cache_key(&self, request: &EnrichmentRequest) -> String {
        // Hash content + profile + schema version
        use std::hash::{Hash, Hasher};
        use std::collections::hash_map::DefaultHasher;

        let mut hasher = DefaultHasher::new();
        request.content.data.hash(&mut hasher);
        request.profile.name.hash(&mut hasher);
        request.target_schema.hash(&mut hasher);

        format!("{:x}", hasher.finish())
    }
}
```

### Batch Optimization

```rust
impl BamlEnrichmentProvider {
    /// Optimized batch processing with deduplication
    pub async fn enrich_batch_optimized(
        &self,
        requests: Vec<EnrichmentRequest>,
    ) -> Result<Vec<KnowledgeObject>, EnrichmentError> {
        // Deduplicate requests by content hash
        let mut unique_requests = HashMap::new();
        let mut request_indices = Vec::new();

        for (idx, request) in requests.into_iter().enumerate() {
            let key = self.content_hash(&request);
            let unique_idx = unique_requests.len();
            unique_requests.entry(key).or_insert((request, unique_idx));
            request_indices.push(unique_idx);
        }

        // Process unique requests in parallel
        let unique_results = self.enrich_batch(
            unique_requests.into_values().map(|(req, _)| req).collect()
        ).await?;

        // Map results back to original order
        Ok(request_indices.iter()
            .map(|&idx| unique_results[idx].clone())
            .collect())
    }
}
```

---

## Monitoring and Observability

### Metrics

```rust
use prometheus::{Counter, Histogram, Registry};

pub struct BamlProviderMetrics {
    requests_total: Counter,
    requests_failed: Counter,
    request_duration: Histogram,
    tokens_consumed: Counter,
    cost_total: Counter,
}

impl BamlProviderMetrics {
    pub fn record_request(&self, duration: Duration, cost: f64, success: bool) {
        self.requests_total.inc();
        if !success {
            self.requests_failed.inc();
        }
        self.request_duration.observe(duration.as_secs_f64());
        self.cost_total.inc_by(cost);
    }
}
```

### Logging

```rust
use tracing::{info, warn, error, instrument};

#[instrument(skip(self, request))]
async fn enrich(&self, request: EnrichmentRequest) -> Result<KnowledgeObject, EnrichmentError> {
    info!(
        provider = self.id(),
        content_size = request.content.metadata.size_bytes,
        profile = request.profile.name,
        "Starting enrichment"
    );

    let result = self.enrich_internal(request).await;

    match &result {
        Ok(obj) => {
            info!(
                confidence = obj.confidence.overall,
                atomic_notes = obj.atomic_notes.len(),
                decisions = obj.decisions.len(),
                tasks = obj.tasks.len(),
                "Enrichment completed successfully"
            );
        }
        Err(e) => {
            error!(error = %e, "Enrichment failed");
        }
    }

    result
}
```

---

## Next Steps

1. **Implement contract types** in `src/enrichment/contract.rs`
2. **Implement BAML provider** following this guide
3. **Write compliance tests** to validate the implementation
4. **Test with real content** and tune prompts
5. **Implement Direct LLM provider** as simpler alternative
6. **Document provider comparison** (cost, quality, latency)

---

## References

- BAML Documentation: https://docs.boundaryml.com/
- `ENRICHMENT-PROVIDER-CONTRACT.md` — Contract specification
- `schemas/knowledge_object.baml` — BAML schema definition
- `docs/ctxt/schema-object.md` — Knowledge object schema

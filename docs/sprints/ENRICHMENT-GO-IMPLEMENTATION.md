# Enrichment Provider Implementation Guide (Go)

**Status:** Draft
**Sprint:** 001 (Reference Implementation)
**Language:** Go
**Related:** `ENRICHMENT-PROVIDER-CONTRACT.md`, `docs/ctxt/pipelines.md`

---

## Overview

This document describes how to implement the Enrichment Provider Contract in Go for the ContextHelp project.

Since BAML doesn't have a native Go runtime, we'll implement **two providers**:

1. **Direct LLM Provider** — Call Anthropic/OpenAI APIs directly (simple, no dependencies)
2. **BAML CLI Provider** — Call BAML as subprocess (optional, for BAML fans)

We'll focus on **Direct LLM Provider** as the reference implementation since it's:
- Simpler (no subprocess management)
- More Go-idiomatic
- Easier to test and debug
- Lower latency (no IPC overhead)

---

## Project Structure

```
ctxt/
├─ pkg/
│  ├─ enrichment/
│  │  ├─ contract.go          # Interface definition
│  │  ├─ types.go             # Shared types
│  │  ├─ errors.go            # Error types
│  │  ├─ registry.go          # Provider registry
│  │  └─ providers/
│  │     ├─ direct.go         # Direct LLM provider (RECOMMENDED)
│  │     ├─ baml.go           # BAML CLI provider (optional)
│  │     └─ hybrid.go         # Hybrid local+cloud
│  ├─ llm/
│  │  ├─ anthropic.go         # Anthropic client wrapper
│  │  └─ openai.go            # OpenAI client wrapper
│  └─ ...
├─ schemas/
│  └─ knowledge_object.json   # JSON Schema for validation
├─ prompts/
│  └─ enrich_content.tmpl     # Go template for enrichment prompt
└─ go.mod
```

---

## Dependencies

```bash
# Add to go.mod
go get github.com/anthropics/anthropic-sdk-go
go get github.com/sashabaranov/go-openai
```

---

## Contract Interface

```go
// pkg/enrichment/contract.go

package enrichment

import (
    "context"
    "time"
)

// Provider is the core interface for all enrichment providers
type Provider interface {
    // ID returns provider identifier (e.g., "direct-anthropic-1.0")
    ID() string

    // Enrich extracts structured knowledge from unstructured content
    Enrich(ctx context.Context, req *Request) (*KnowledgeObject, error)

    // Capabilities describes what this provider can do
    Capabilities() Capabilities

    // EstimateCost estimates cost before execution
    EstimateCost(req *Request) Cost

    // EnrichBatch processes multiple requests (optional)
    EnrichBatch(ctx context.Context, reqs []*Request) ([]*KnowledgeObject, error)
}
```

---

## Core Types

```go
// pkg/enrichment/types.go

package enrichment

import "time"

// Request represents input to enrichment
type Request struct {
    Content         Content
    TargetSchema    SchemaVersion
    Context         Context
    Profile         FocusProfile
    RegistryHints   []RegistryHint
    PriorExtraction *KnowledgeObject // For re-enrichment
}

// Content is unstructured input
type Content struct {
    Format   Format
    Text     string        // For text-based content
    Binary   []byte        // For binary content
    URL      string        // For URL-based content
    Metadata ContentMeta
}

type Format string

const (
    FormatPlainText Format = "text"
    FormatMarkdown  Format = "markdown"
    FormatHTML      Format = "html"
)

type ContentMeta struct {
    SourceURL   string
    Title       string
    Author      string
    PublishedAt *time.Time
    Language    string
    SizeBytes   int
}

// Context provides execution constraints
type Context struct {
    BudgetLimit      *float64       // Max cost in USD
    QualityThreshold float64        // Min confidence (0.0-1.0)
    MaxLatency       *time.Duration
    PrivacyMode      bool
}

type FocusProfile struct {
    Name string // "Founder", "Engineer", "Research"
}

type RegistryHint struct {
    Mention string // e.g., "@company.anthropic"
    Type    string
}

type SchemaVersion string

const (
    SchemaV1 SchemaVersion = "v1"
)

// KnowledgeObject is structured output
type KnowledgeObject struct {
    Summary      string
    AtomicNotes  []AtomicNote
    Tags         []Tag
    Mentions     []Mention
    Decisions    []Decision
    Tasks        []Task
    Questions    []Question
    Confidence   Confidence
    Provenance   Provenance
}

type AtomicNote struct {
    Content    string
    Source     string  // Location in original content
    Confidence float64
    Tags       []string
}

type Decision struct {
    What                   string
    Rationale              string
    Stakeholders           []string
    AlternativesConsidered []string
    Confidence             float64
}

type Task struct {
    Description  string
    Priority     string
    Assignee     string
    DueDate      *time.Time
    Dependencies []string
    Confidence   float64
}

type Mention struct {
    Text          string
    Type          string // "person", "company", "concept", etc.
    Namespace     string
    Slug          string
    Confidence    float64
    CanonicalName string
}

type Question struct {
    Question         string
    Context          string
    PotentialAnswers []string
    Confidence       float64
}

type Tag struct {
    Label      string
    Namespace  string
    Confidence float64
    Weight     float64
}

type Confidence struct {
    Overall          float64
    Summary          float64
    Extractions      float64
    EntityResolution float64
}

type Provenance struct {
    EnrichedBy          string
    ProviderVersion     string
    Model               string
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
    Input  int
    Output int
}

// Capabilities describes provider capabilities
type Capabilities struct {
    SupportsMultimodal bool
    MaxContentSize     int
    SupportedFormats   []Format
    RequiresNetwork    bool
    CostModel          CostModel
    QualityProfile     QualityProfile
}

type CostModel struct {
    InputCostPerToken  float64
    OutputCostPerToken float64
}

type QualityProfile struct {
    TypicalQuality float64
    TypicalLatency time.Duration
    Reliability    float64
}
```

---

## Error Types

```go
// pkg/enrichment/errors.go

package enrichment

import "fmt"

type ProviderError struct {
    ProviderID string
    Message    string
    Cause      error
}

func (e *ProviderError) Error() string {
    return fmt.Sprintf("provider %s: %s", e.ProviderID, e.Message)
}

func (e *ProviderError) Unwrap() error {
    return e.Cause
}

type ContentTooLargeError struct {
    Size int
    Max  int
}

func (e *ContentTooLargeError) Error() string {
    return fmt.Sprintf("content too large: %d bytes (max: %d)", e.Size, e.Max)
}

type BudgetExceededError struct {
    Estimated float64
    Limit     float64
}

func (e *BudgetExceededError) Error() string {
    return fmt.Sprintf("budget exceeded: $%.4f (limit: $%.4f)", e.Estimated, e.Limit)
}

type QualityTooLowError struct {
    Quality   float64
    Threshold float64
}

func (e *QualityTooLowError) Error() string {
    return fmt.Sprintf("quality too low: %.2f (min: %.2f)", e.Quality, e.Threshold)
}
```

---

## Direct LLM Provider Implementation

```go
// pkg/enrichment/providers/direct.go

package providers

import (
    "context"
    "encoding/json"
    "fmt"
    "strings"
    "text/template"
    "time"

    "github.com/anthropics/anthropic-sdk-go"
    "github.com/ideacrafterslabs/ctxt/pkg/enrichment"
)

// DirectProvider calls LLM APIs directly
type DirectProvider struct {
    anthropic      *anthropic.Client
    promptTemplate *template.Template
    model          string
}

// NewDirectProvider creates a new direct LLM provider
func NewDirectProvider(apiKey, model string) (*DirectProvider, error) {
    client := anthropic.NewClient(
        anthropic.WithAPIKey(apiKey),
    )

    // Load prompt template
    tmpl, err := template.ParseFiles("prompts/enrich_content.tmpl")
    if err != nil {
        return nil, fmt.Errorf("load prompt template: %w", err)
    }

    return &DirectProvider{
        anthropic:      client,
        promptTemplate: tmpl,
        model:          model,
    }, nil
}

func (p *DirectProvider) ID() string {
    return "direct-anthropic-1.0"
}

func (p *DirectProvider) Enrich(ctx context.Context, req *enrichment.Request) (*enrichment.KnowledgeObject, error) {
    start := time.Now()

    // Check capabilities
    caps := p.Capabilities()
    if req.Content.Metadata.SizeBytes > caps.MaxContentSize {
        return nil, &enrichment.ContentTooLargeError{
            Size: req.Content.Metadata.SizeBytes,
            Max:  caps.MaxContentSize,
        }
    }

    // Check budget
    estimated := p.EstimateCost(req)
    if req.Context.BudgetLimit != nil && estimated.USD > *req.Context.BudgetLimit {
        return nil, &enrichment.BudgetExceededError{
            Estimated: estimated.USD,
            Limit:     *req.Context.BudgetLimit,
        }
    }

    // Render prompt
    prompt, err := p.renderPrompt(req)
    if err != nil {
        return nil, fmt.Errorf("render prompt: %w", err)
    }

    // Call Anthropic API with structured output
    resp, err := p.anthropic.Messages.New(ctx, anthropic.MessageNewParams{
        Model: anthropic.String(p.model),
        Messages: []anthropic.MessageParam{
            anthropic.NewUserMessage(anthropic.NewTextBlock(prompt)),
        },
        MaxTokens: anthropic.Int(4096),
    })
    if err != nil {
        return nil, &enrichment.ProviderError{
            ProviderID: p.ID(),
            Message:    "API call failed",
            Cause:      err,
        }
    }

    // Parse structured output
    obj, err := p.parseResponse(resp)
    if err != nil {
        return nil, fmt.Errorf("parse response: %w", err)
    }

    // Check quality threshold
    if obj.Confidence.Overall < req.Context.QualityThreshold {
        return nil, &enrichment.QualityTooLowError{
            Quality:   obj.Confidence.Overall,
            Threshold: req.Context.QualityThreshold,
        }
    }

    // Add provenance
    obj.Provenance = enrichment.Provenance{
        EnrichedBy:      p.ID(),
        ProviderVersion: "1.0.0",
        Model:           p.model,
        EnrichedAt:      time.Now(),
        SchemaVersion:   req.TargetSchema,
        Cost:            &estimated,
        Tokens: &enrichment.TokenUsage{
            Input:  int(resp.Usage.InputTokens),
            Output: int(resp.Usage.OutputTokens),
        },
        Latency: time.Since(start),
        CompatibleProviders: []string{
            "direct-anthropic-1.1",
            "direct-openai-1.0",
        },
    }

    return obj, nil
}

func (p *DirectProvider) Capabilities() enrichment.Capabilities {
    return enrichment.Capabilities{
        SupportsMultimodal: false,
        MaxContentSize:     800_000, // ~200k tokens
        SupportedFormats: []enrichment.Format{
            enrichment.FormatPlainText,
            enrichment.FormatMarkdown,
        },
        RequiresNetwork: true,
        CostModel: enrichment.CostModel{
            InputCostPerToken:  0.000003,
            OutputCostPerToken: 0.000015,
        },
        QualityProfile: enrichment.QualityProfile{
            TypicalQuality: 0.82,
            TypicalLatency: 3 * time.Second,
            Reliability:    0.99,
        },
    }
}

func (p *DirectProvider) EstimateCost(req *enrichment.Request) enrichment.Cost {
    // Rough: 1 char ≈ 0.25 tokens
    inputTokens := req.Content.Metadata.SizeBytes / 4

    // Structured output typically 1000-3000 tokens
    outputTokens := 2000

    cost := float64(inputTokens)*0.000003 + float64(outputTokens)*0.000015

    return enrichment.Cost{
        USD: cost,
        Tokens: &enrichment.TokenUsage{
            Input:  inputTokens,
            Output: outputTokens,
        },
    }
}

func (p *DirectProvider) EnrichBatch(ctx context.Context, reqs []*enrichment.Request) ([]*enrichment.KnowledgeObject, error) {
    results := make([]*enrichment.KnowledgeObject, len(reqs))
    errChan := make(chan error, len(reqs))

    for i, req := range reqs {
        go func(idx int, r *enrichment.Request) {
            obj, err := p.Enrich(ctx, r)
            if err != nil {
                errChan <- err
                return
            }
            results[idx] = obj
            errChan <- nil
        }(i, req)
    }

    for i := 0; i < len(reqs); i++ {
        if err := <-errChan; err != nil {
            return nil, err
        }
    }

    return results, nil
}

// renderPrompt renders the enrichment prompt template
func (p *DirectProvider) renderPrompt(req *enrichment.Request) (string, error) {
    var buf strings.Builder

    data := map[string]interface{}{
        "Content":      req.Content.Text,
        "ContentType":  req.Content.Format,
        "Profile":      req.Profile.Name,
        "RegistryHints": req.RegistryHints,
    }

    if err := p.promptTemplate.Execute(&buf, data); err != nil {
        return "", err
    }

    return buf.String(), nil
}

// parseResponse extracts KnowledgeObject from LLM response
func (p *DirectProvider) parseResponse(resp *anthropic.Message) (*enrichment.KnowledgeObject, error) {
    // Expect JSON in response
    var content string
    for _, block := range resp.Content {
        if block.Type == "text" {
            content = block.Text
            break
        }
    }

    // Extract JSON from markdown code blocks if present
    content = extractJSON(content)

    var obj enrichment.KnowledgeObject
    if err := json.Unmarshal([]byte(content), &obj); err != nil {
        return nil, fmt.Errorf("unmarshal response: %w", err)
    }

    return &obj, nil
}

// extractJSON extracts JSON from markdown code blocks
func extractJSON(text string) string {
    // Look for ```json ... ```
    if strings.Contains(text, "```json") {
        start := strings.Index(text, "```json") + 7
        end := strings.Index(text[start:], "```")
        if end > 0 {
            return strings.TrimSpace(text[start : start+end])
        }
    }

    // Look for ``` ... ```
    if strings.Contains(text, "```") {
        start := strings.Index(text, "```") + 3
        end := strings.Index(text[start:], "```")
        if end > 0 {
            return strings.TrimSpace(text[start : start+end])
        }
    }

    // Assume entire text is JSON
    return strings.TrimSpace(text)
}
```

---

## Prompt Template

```go
// prompts/enrich_content.tmpl

You are enriching content for ContextHelp, a personal knowledge management system.

**User Profile:** {{ .Profile }}
**Content Type:** {{ .ContentType }}

{{ if .RegistryHints }}
**Known Entities:**
{{ range .RegistryHints }}
- {{ .Mention }} ({{ .Type }})
{{ end }}
{{ end }}

**Content:**
---
{{ .Content }}
---

Extract ALL structured information into JSON format matching this schema:

{
  "summary": "2-3 sentence summary",
  "atomic_notes": [
    {
      "content": "discrete fact or insight",
      "source": "location in content",
      "confidence": 0.85,
      "tags": ["tag1"]
    }
  ],
  "tags": [
    {
      "label": "tag-name",
      "namespace": "taxonomy-name",
      "confidence": 0.9,
      "weight": 1.0
    }
  ],
  "mentions": [
    {
      "text": "original text",
      "type": "person|company|concept|location|product",
      "namespace": "namespace",
      "slug": "kebab-case-slug",
      "confidence": 0.9,
      "canonical_name": "Full Name"
    }
  ],
  "decisions": [
    {
      "what": "decision made",
      "rationale": "why",
      "stakeholders": ["who"],
      "alternatives_considered": ["option1"],
      "confidence": 0.8
    }
  ],
  "tasks": [
    {
      "description": "what needs to be done",
      "priority": "high|medium|low",
      "assignee": "person",
      "due_date": "2024-01-15T00:00:00Z",
      "dependencies": ["other-task"],
      "confidence": 0.85
    }
  ],
  "questions": [
    {
      "question": "open question",
      "context": "why it matters",
      "potential_answers": ["answer1"],
      "confidence": 0.7
    }
  ],
  "confidence": {
    "overall": 0.85,
    "summary": 0.9,
    "extractions": 0.8,
    "entity_resolution": 0.75
  }
}

**Guidelines:**
- Be comprehensive but precise
- Don't hallucinate
- Use high confidence (0.8+) only for explicit information
- Medium confidence (0.5-0.7) for inferred information
- Low confidence (<0.5) for uncertain extractions
- Extract mentions using @namespace.slug format
- For {{ .Profile }} profile, focus on relevant dimensions

Return ONLY valid JSON.
```

---

## Provider Registry

```go
// pkg/enrichment/registry.go

package enrichment

import (
    "context"
    "fmt"
    "sync"
)

// Registry manages available providers
type Registry struct {
    mu               sync.RWMutex
    providers        map[string]Provider
    defaultProvider  string
}

// NewRegistry creates a new provider registry
func NewRegistry() *Registry {
    return &Registry{
        providers: make(map[string]Provider),
    }
}

// Register adds a provider to the registry
func (r *Registry) Register(p Provider) {
    r.mu.Lock()
    defer r.mu.Unlock()
    r.providers[p.ID()] = p
}

// SetDefault sets the default provider
func (r *Registry) SetDefault(id string) error {
    r.mu.Lock()
    defer r.mu.Unlock()

    if _, exists := r.providers[id]; !exists {
        return fmt.Errorf("provider %s not found", id)
    }

    r.defaultProvider = id
    return nil
}

// Get retrieves a provider by ID
func (r *Registry) Get(id string) (Provider, error) {
    r.mu.RLock()
    defer r.mu.RUnlock()

    p, exists := r.providers[id]
    if !exists {
        return nil, fmt.Errorf("provider %s not found", id)
    }

    return p, nil
}

// Default returns the default provider
func (r *Registry) Default() (Provider, error) {
    r.mu.RLock()
    defer r.mu.RUnlock()

    if r.defaultProvider == "" {
        return nil, fmt.Errorf("no default provider set")
    }

    return r.providers[r.defaultProvider], nil
}

// Select chooses a provider based on request characteristics
func (r *Registry) Select(ctx context.Context, req *Request) (Provider, error) {
    // For now, just return default
    // Future: implement smart routing based on content type, size, budget, etc.
    return r.Default()
}
```

---

## Usage Example

```go
// Example: Using the enrichment system

package main

import (
    "context"
    "fmt"
    "log"
    "os"

    "github.com/ideacrafterslabs/ctxt/pkg/enrichment"
    "github.com/ideacrafterslabs/ctxt/pkg/enrichment/providers"
)

func main() {
    // Create provider
    provider, err := providers.NewDirectProvider(
        os.Getenv("ANTHROPIC_API_KEY"),
        "claude-sonnet-4",
    )
    if err != nil {
        log.Fatal(err)
    }

    // Create enrichment request
    req := &enrichment.Request{
        Content: enrichment.Content{
            Format: enrichment.FormatPlainText,
            Text: `Anthropic released Claude 3.5 Sonnet today.
                   The new model shows significant improvements in coding
                   and analysis. Pricing remains $3/MTok input.`,
            Metadata: enrichment.ContentMeta{
                SizeBytes: 150,
            },
        },
        TargetSchema: enrichment.SchemaV1,
        Context: enrichment.Context{
            QualityThreshold: 0.7,
        },
        Profile: enrichment.FocusProfile{
            Name: "Engineer",
        },
        RegistryHints: []enrichment.RegistryHint{
            {Mention: "@company.anthropic", Type: "company"},
            {Mention: "@product.claude", Type: "product"},
        },
    }

    // Enrich
    ctx := context.Background()
    obj, err := provider.Enrich(ctx, req)
    if err != nil {
        log.Fatal(err)
    }

    // Display results
    fmt.Printf("Summary: %s\n", obj.Summary)
    fmt.Printf("Confidence: %.2f\n", obj.Confidence.Overall)
    fmt.Printf("Atomic Notes: %d\n", len(obj.AtomicNotes))
    fmt.Printf("Mentions: %d\n", len(obj.Mentions))
    fmt.Printf("Cost: $%.4f\n", obj.Provenance.Cost.USD)
}
```

---

## Testing

```go
// pkg/enrichment/providers/direct_test.go

package providers

import (
    "context"
    "testing"

    "github.com/ideacrafterslabs/ctxt/pkg/enrichment"
)

func TestDirectProvider_Enrich(t *testing.T) {
    // Skip if no API key
    apiKey := os.Getenv("ANTHROPIC_API_KEY")
    if apiKey == "" {
        t.Skip("ANTHROPIC_API_KEY not set")
    }

    provider, err := NewDirectProvider(apiKey, "claude-sonnet-4")
    if err != nil {
        t.Fatal(err)
    }

    req := &enrichment.Request{
        Content: enrichment.Content{
            Format: enrichment.FormatPlainText,
            Text:   "Test article about AI.",
            Metadata: enrichment.ContentMeta{
                SizeBytes: 25,
            },
        },
        TargetSchema: enrichment.SchemaV1,
        Context: enrichment.Context{
            QualityThreshold: 0.0,
        },
        Profile: enrichment.FocusProfile{Name: "Engineer"},
    }

    obj, err := provider.Enrich(context.Background(), req)
    if err != nil {
        t.Fatal(err)
    }

    if obj.Summary == "" {
        t.Error("expected non-empty summary")
    }

    if obj.Confidence.Overall == 0 {
        t.Error("expected non-zero confidence")
    }

    if obj.Provenance.EnrichedBy != provider.ID() {
        t.Errorf("expected provider ID %s, got %s", provider.ID(), obj.Provenance.EnrichedBy)
    }
}

func TestDirectProvider_BudgetEnforcement(t *testing.T) {
    provider, _ := NewDirectProvider("test", "test")

    budget := 0.001 // Very low budget
    req := &enrichment.Request{
        Content: enrichment.Content{
            Text: string(make([]byte, 100_000)), // Large content
            Metadata: enrichment.ContentMeta{
                SizeBytes: 100_000,
            },
        },
        Context: enrichment.Context{
            BudgetLimit: &budget,
        },
    }

    _, err := provider.Enrich(context.Background(), req)

    var budgetErr *enrichment.BudgetExceededError
    if !errors.As(err, &budgetErr) {
        t.Errorf("expected BudgetExceededError, got %v", err)
    }
}
```

---

## Next Steps

1. **Implement contract types** (`pkg/enrichment/*.go`)
2. **Implement DirectProvider** (`pkg/enrichment/providers/direct.go`)
3. **Create prompt template** (`prompts/enrich_content.tmpl`)
4. **Write tests**
5. **Integrate with ctxt pipeline engine**
6. **Add configuration support** (Viper)
7. **Optional: Add BAML CLI provider** for comparison

---

## Why Not BAML for Go?

BAML is excellent but:
- No native Go runtime (would require subprocess or FFI)
- Adds complexity (IPC, schema compilation)
- Go's `encoding/json` and LLM structured outputs work well
- Direct implementation is more debuggable

**Recommendation:** Start with Direct provider. Add BAML later if needed for advanced features (versioned prompts, complex type validation).

The contract design still allows swapping to BAML later without breaking changes.

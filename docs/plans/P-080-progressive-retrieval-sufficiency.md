# Progressive Retrieval Sufficiency Checking

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Implement a tiered retrieval system with progressive sufficiency checking that stops early when enough context is found, reducing latency and token costs.

**Reference:** Based on memU's approach from https://github.com/NevaMind-AI/memU

---

## Overview

### Problem
Current retrieval systems fetch all potentially relevant data upfront, even when a simple query might be answerable with just category summaries. This wastes:
- **Latency** - fetching items/resources when categories suffice
- **Tokens** - passing unnecessary context to LLM
- **Cost** - extra embedding lookups and LLM calls

### Solution
Progressive retrieval with sufficiency checking at each tier:

```
Query → Categories → [SUFFICIENT?] ──YES──→ Return
                │
               NO
                ↓
           Items → [SUFFICIENT?] ──YES──→ Return
                │
               NO
                ↓
          Resources → Return
```

---

## Architecture

### Tiers

| Tier | Content | When Used |
|------|---------|-----------|
| Categories | High-level topic summaries | First pass, good for "what do you know about X?" |
| Items | Specific facts/memories | When categories are too vague |
| Resources | Original documents | When need source verification or full context |

### Sufficiency Check

After each tier, an LLM evaluates:
1. Is the retrieved content sufficient to answer the query?
2. If not, what specific information is still needed?
3. Rewrite the query to be more targeted for the next tier

---

## Files to Create/Modify

```
internal/
├── retrieval/                          # NEW PACKAGE
│   ├── doc.go                          # Package documentation
│   ├── config.go                       # Configuration types
│   ├── types.go                        # Core types (state, results)
│   ├── workflow.go                     # Workflow orchestrator
│   ├── sufficiency.go                  # LLM-based sufficiency checker
│   ├── prompts.go                      # Prompt templates
│   ├── retriever.go                    # Tier-specific retrievers
│   ├── retriever_test.go
│   ├── sufficiency_test.go
│   └── workflow_test.go
├── providers/
│   └── llm.go                          # NEW: LLM provider interface
├── providers/
│   └── llm_openai.go                   # NEW: OpenAI implementation
└── search/
    └── engine.go                       # MODIFY: Add retrieval integration
```

---

## Implementation Tasks

### Task 1: Core Types

**Files:**
- Create: `internal/retrieval/types.go`

```go
package retrieval

import (
    "github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Method determines how retrieval is performed
type Method string

const (
    MethodRAG Method = "rag"  // Vector similarity search
    MethodLLM Method = "llm"  // LLM-based ranking
)

// Config for progressive retrieval
type Config struct {
    Method           Method
    EnableSufficiencyCheck bool
    LLMProfile       string  // Which LLM profile to use for checking
    
    Categories TierConfig
    Items      TierConfig
    Resources  TierConfig
}

// TierConfig for each retrieval tier
type TierConfig struct {
    Enabled bool
    TopK    int
}

// State tracks retrieval progress
type State struct {
    OriginalQuery   string
    RewrittenQuery  string
    ActiveQuery     string
    NeedsRetrieval  bool
    ProceedToItems  bool
    ProceedToResources bool
    
    CategoryHits []Hit
    ItemHits     []Hit
    ResourceHits []Hit
    
    QueryVector []float32
    NextStepQuery string
}

// Hit represents a retrieved item with score
type Hit struct {
    ID    string
    Score float64
    Data  *storage.KnowledgeObject
}

// Result is the final retrieval output
type Result struct {
    NeedsRetrieval  bool
    OriginalQuery   string
    RewrittenQuery  string
    NextStepQuery   string
    Categories      []*storage.KnowledgeObject
    Items           []*storage.KnowledgeObject
    Resources       []*storage.KnowledgeObject
}
```

---

### Task 2: LLM Provider Interface

**Files:**
- Create: `internal/providers/llm.go`

```go
package providers

import "context"

// LLMProvider defines the interface for LLM operations
type LLMProvider interface {
    // Chat sends a message and returns the response
    Chat(ctx context.Context, prompt string, opts ...ChatOption) (string, error)
    
    // ChatWithSystem sends a message with system prompt
    ChatWithSystem(ctx context.Context, system, user string, opts ...ChatOption) (string, error)
}

// ChatOption configures chat behavior
type ChatOption func(*ChatConfig)

type ChatConfig struct {
    Temperature float64
    MaxTokens   int
    Model       string
}

// WithTemperature sets the sampling temperature
func WithTemperature(t float64) ChatOption {
    return func(c *ChatConfig) { c.Temperature = t }
}

// WithMaxTokens sets max output tokens
func WithMaxTokens(n int) ChatOption {
    return func(c *ChatConfig) { c.MaxTokens = n }
}
```

---

### Task 3: Sufficiency Checker

**Files:**
- Create: `internal/retrieval/sufficiency.go`
- Create: `internal/retrieval/prompts.go`

**prompts.go:**
```go
package retrieval

const SufficiencySystemPrompt = `# Task Objective
Determine whether the retrieved content is sufficient to answer the query.
If insufficient, rewrite the query to be more targeted for additional retrieval.

# Decision Rules
- NO_RETRIEVE if:
  - Content fully answers the query
  - Query is a greeting or casual chat
  - Query only references current conversation context
- RETRIEVE if:
  - Key information is missing
  - Content is too vague or generic
  - More specific details are needed

# Output Format
<decision>RETRIEVE or NO_RETRIEVE</decision>
<rewritten_query>
If RETRIEVE: A more specific query incorporating context
If NO_RETRIEVE: The original query unchanged
</rewritten_query>`

const SufficiencyUserPrompt = `# Input
## Query Context
{conversation_history}

## Current Query
{query}

## Retrieved Content
{retrieved_content}`
```

**sufficiency.go:**
```go
package retrieval

import (
    "context"
    "regexp"
    "strings"
    
    "github.com/ideacrafterslabs/ctxt/internal/providers"
)

type SufficiencyChecker struct {
    llm providers.LLMProvider
}

func NewSufficiencyChecker(llm providers.LLMProvider) *SufficiencyChecker {
    return &SufficiencyChecker{llm: llm}
}

// Check evaluates if retrieved content is sufficient
func (c *SufficiencyChecker) Check(
    ctx context.Context,
    query string,
    conversationHistory []string,
    retrievedContent string,
) (needsMore bool, rewrittenQuery string, err error) {
    prompt := buildSufficiencyPrompt(query, conversationHistory, retrievedContent)
    
    response, err := c.llm.ChatWithSystem(ctx, SufficiencySystemPrompt, prompt)
    if err != nil {
        return true, query, err
    }
    
    decision := extractDecision(response)
    rewritten := extractRewrittenQuery(response)
    if rewritten == "" {
        rewritten = query
    }
    
    return decision == "RETRIEVE", rewritten, nil
}

func buildSufficiencyPrompt(query string, history []string, content string) string {
    historyText := "No prior context."
    if len(history) > 0 {
        historyText = strings.Join(history, "\n")
    }
    
    return strings.ReplaceAll(
        strings.ReplaceAll(
            strings.ReplaceAll(SufficiencyUserPrompt,
                "{conversation_history}", historyText),
            "{query}", query),
        "{retrieved_content}", content)
}

func extractDecision(response string) string {
    re := regexp.MustCompile(`(?i)<decision>\s*(.*?)\s*</decision>`)
    if match := re.FindStringSubmatch(response); match != nil {
        decision := strings.ToUpper(strings.TrimSpace(match[1]))
        if strings.Contains(decision, "NO_RETRIEVE") {
            return "NO_RETRIEVE"
        }
        if strings.Contains(decision, "RETRIEVE") {
            return "RETRIEVE"
        }
    }
    
    // Fallback: check raw text
    upper := strings.ToUpper(response)
    if strings.Contains(upper, "NO_RETRIEVE") {
        return "NO_RETRIEVE"
    }
    return "RETRIEVE"
}

func extractRewrittenQuery(response string) string {
    re := regexp.MustCompile(`(?i)<rewritten_query>\s*(.*?)\s*</rewritten_query>`)
    if match := re.FindStringSubmatch(response); match != nil {
        return strings.TrimSpace(match[1])
    }
    return ""
}
```

---

### Task 4: Workflow Orchestrator

**Files:**
- Create: `internal/retrieval/workflow.go`

```go
package retrieval

import (
    "context"
    "fmt"
    
    "github.com/ideacrafterslabs/ctxt/internal/providers"
    "github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Workflow orchestrates progressive retrieval
type Workflow struct {
    config    Config
    store     storage.StorageDriver
    llm       providers.LLMProvider
    embedding EmbeddingProvider // For RAG method
}

//go:generate mockgen -source=workflow.go -destination=mock_test.go -package=retrieval

type EmbeddingProvider interface {
    Embed(ctx context.Context, texts []string) ([][]float32, error)
}

// NewWorkflow creates a new retrieval workflow
func NewWorkflow(
    config Config,
    store storage.StorageDriver,
    llm providers.LLMProvider,
    embedding EmbeddingProvider,
) *Workflow {
    return &Workflow{
        config:    config,
        store:     store,
        llm:       llm,
        embedding: embedding,
    }
}

// Retrieve executes progressive retrieval with sufficiency checking
func (w *Workflow) Retrieve(
    ctx context.Context,
    query string,
    conversationHistory []string,
    filter storage.ObjectFilter,
) (*Result, error) {
    state := &State{
        OriginalQuery:  query,
        RewrittenQuery: query,
        ActiveQuery:    query,
        NeedsRetrieval: true,
    }
    
    // Step 1: Route intention (optional)
    if w.config.EnableSufficiencyCheck {
        needsRetrieval, rewritten, err := w.checkRouteIntention(ctx, query, conversationHistory)
        if err != nil {
            return nil, fmt.Errorf("route intention: %w", err)
        }
        state.NeedsRetrieval = needsRetrieval
        state.RewrittenQuery = rewritten
        state.ActiveQuery = rewritten
    }
    
    if !state.NeedsRetrieval {
        return w.buildResult(state), nil
    }
    
    // Step 2: Tier 1 - Categories
    if w.config.Categories.Enabled {
        if err := w.retrieveCategories(ctx, state, filter); err != nil {
            return nil, fmt.Errorf("retrieve categories: %w", err)
        }
        
        if w.config.EnableSufficiencyCheck && len(state.CategoryHits) > 0 {
            needsMore, rewritten, err := w.checkSufficiency(ctx, state, conversationHistory)
            if err != nil {
                return nil, fmt.Errorf("sufficiency check after categories: %w", err)
            }
            state.ProceedToItems = needsMore
            state.ActiveQuery = rewritten
            state.NextStepQuery = rewritten
            
            if !needsMore {
                return w.buildResult(state), nil
            }
        } else {
            state.ProceedToItems = true
        }
    }
    
    // Step 3: Tier 2 - Items
    if w.config.Items.Enabled && state.ProceedToItems {
        if err := w.retrieveItems(ctx, state, filter); err != nil {
            return nil, fmt.Errorf("retrieve items: %w", err)
        }
        
        if w.config.EnableSufficiencyCheck && len(state.ItemHits) > 0 {
            needsMore, rewritten, err := w.checkSufficiency(ctx, state, conversationHistory)
            if err != nil {
                return nil, fmt.Errorf("sufficiency check after items: %w", err)
            }
            state.ProceedToResources = needsMore
            state.ActiveQuery = rewritten
            state.NextStepQuery = rewritten
            
            if !needsMore {
                return w.buildResult(state), nil
            }
        } else {
            state.ProceedToResources = true
        }
    }
    
    // Step 4: Tier 3 - Resources
    if w.config.Resources.Enabled && state.ProceedToResources {
        if err := w.retrieveResources(ctx, state, filter); err != nil {
            return nil, fmt.Errorf("retrieve resources: %w", err)
        }
    }
    
    return w.buildResult(state), nil
}

func (w *Workflow) checkRouteIntention(
    ctx context.Context,
    query string,
    history []string,
) (bool, string, error) {
    checker := NewSufficiencyChecker(w.llm)
    return checker.Check(ctx, query, history, "No content retrieved yet.")
}

func (w *Workflow) checkSufficiency(
    ctx context.Context,
    state *State,
    history []string,
) (bool, string, error) {
    content := w.formatRetrievedContent(state)
    checker := NewSufficiencyChecker(w.llm)
    return checker.Check(ctx, state.ActiveQuery, history, content)
}

func (w *Workflow) formatRetrievedContent(state *State) string {
    var parts []string
    
    if len(state.CategoryHits) > 0 {
        parts = append(parts, "## Categories")
        for _, hit := range state.CategoryHits {
            if hit.Data != nil {
                parts = append(parts, fmt.Sprintf("- %s (score: %.3f)", 
                    hit.Data.Summaries[0], hit.Score))
            }
        }
    }
    
    if len(state.ItemHits) > 0 {
        parts = append(parts, "\n## Items")
        for _, hit := range state.ItemHits {
            if hit.Data != nil {
                parts = append(parts, fmt.Sprintf("- %s (score: %.3f)",
                    hit.Data.RawContent[:min(200, len(hit.Data.RawContent))], hit.Score))
            }
        }
    }
    
    if len(parts) == 0 {
        return "No content retrieved yet."
    }
    return strings.Join(parts, "\n")
}

func (w *Workflow) buildResult(state *State) *Result {
    result := &Result{
        NeedsRetrieval: state.NeedsRetrieval,
        OriginalQuery:  state.OriginalQuery,
        RewrittenQuery: state.RewrittenQuery,
        NextStepQuery:  state.NextStepQuery,
    }
    
    for _, hit := range state.CategoryHits {
        if hit.Data != nil {
            result.Categories = append(result.Categories, hit.Data)
        }
    }
    for _, hit := range state.ItemHits {
        if hit.Data != nil {
            result.Items = append(result.Items, hit.Data)
        }
    }
    for _, hit := range state.ResourceHits {
        if hit.Data != nil {
            result.Resources = append(result.Resources, hit.Data)
        }
    }
    
    return result
}

func min(a, b int) int {
    if a < b {
        return a
    }
    return b
}
```

---

### Task 5: Tier Retrievers

**Files:**
- Create: `internal/retrieval/retriever.go`

```go
package retrieval

import (
    "context"
    
    "github.com/ideacrafterslabs/ctxt/internal/storage"
)

// retrieveCategories fetches category summaries via vector search
func (w *Workflow) retrieveCategories(
    ctx context.Context,
    state *State,
    filter storage.ObjectFilter,
) error {
    if w.config.Method == MethodRAG {
        return w.ragRetrieveCategories(ctx, state, filter)
    }
    return w.llmRetrieveCategories(ctx, state, filter)
}

func (w *Workflow) ragRetrieveCategories(
    ctx context.Context,
    state *State,
    filter storage.ObjectFilter,
) error {
    // Get query embedding
    embeddings, err := w.embedding.Embed(ctx, []string{state.ActiveQuery})
    if err != nil {
        return err
    }
    state.QueryVector = embeddings[0]
    
    // Vector search for category-like objects (summaries)
    filter.Type = "category" // Or use tags/summaries
    filter.Limit = w.config.Categories.TopK
    
    hits, err := w.store.Objects().VectorSearch(ctx, state.QueryVector, filter)
    if err != nil {
        return err
    }
    
    for _, obj := range hits {
        state.CategoryHits = append(state.CategoryHits, Hit{
            ID:    obj.ID,
            Score: obj.Metadata["score"].(float64),
            Data:  obj,
        })
    }
    return nil
}

func (w *Workflow) llmRetrieveCategories(
    ctx context.Context,
    state *State,
    filter storage.ObjectFilter,
) error {
    // TODO: LLM-based ranking of categories
    // For now, fall back to RAG
    return w.ragRetrieveCategories(ctx, state, filter)
}

// retrieveItems fetches specific memory items
func (w *Workflow) retrieveItems(
    ctx context.Context,
    state *State,
    filter storage.ObjectFilter,
) error {
    if w.config.Method == MethodRAG {
        return w.ragRetrieveItems(ctx, state, filter)
    }
    return w.llmRetrieveItems(ctx, state, filter)
}

func (w *Workflow) ragRetrieveItems(
    ctx context.Context,
    state *State,
    filter storage.ObjectFilter,
) error {
    // Re-embed if query was rewritten
    if len(state.QueryVector) == 0 {
        embeddings, err := w.embedding.Embed(ctx, []string{state.ActiveQuery})
        if err != nil {
            return err
        }
        state.QueryVector = embeddings[0]
    }
    
    filter.Type = "item" // Or relevant subtype
    filter.Limit = w.config.Items.TopK
    
    hits, err := w.store.Objects().VectorSearch(ctx, state.QueryVector, filter)
    if err != nil {
        return err
    }
    
    for _, obj := range hits {
        state.ItemHits = append(state.ItemHits, Hit{
            ID:    obj.ID,
            Score: obj.Metadata["score"].(float64),
            Data:  obj,
        })
    }
    return nil
}

func (w *Workflow) llmRetrieveItems(
    ctx context.Context,
    state *State,
    filter storage.ObjectFilter,
) error {
    // TODO: LLM-based ranking of items
    return w.ragRetrieveItems(ctx, state, filter)
}

// retrieveResources fetches source documents
func (w *Workflow) retrieveResources(
    ctx context.Context,
    state *State,
    filter storage.ObjectFilter,
) error {
    if w.config.Method == MethodRAG {
        return w.ragRetrieveResources(ctx, state, filter)
    }
    return w.llmRetrieveResources(ctx, state, filter)
}

func (w *Workflow) ragRetrieveResources(
    ctx context.Context,
    state *State,
    filter storage.ObjectFilter,
) error {
    if len(state.QueryVector) == 0 {
        embeddings, err := w.embedding.Embed(ctx, []string{state.ActiveQuery})
        if err != nil {
            return err
        }
        state.QueryVector = embeddings[0]
    }
    
    filter.Type = "document"
    filter.Limit = w.config.Resources.TopK
    
    hits, err := w.store.Objects().VectorSearch(ctx, state.QueryVector, filter)
    if err != nil {
        return err
    }
    
    for _, obj := range hits {
        state.ResourceHits = append(state.ResourceHits, Hit{
            ID:    obj.ID,
            Score: obj.Metadata["score"].(float64),
            Data:  obj,
        })
    }
    return nil
}

func (w *Workflow) llmRetrieveResources(
    ctx context.Context,
    state *State,
    filter storage.ObjectFilter,
) error {
    return w.ragRetrieveResources(ctx, state, filter)
}
```

---

### Task 6: Storage Interface Extension

**Files:**
- Modify: `internal/storage/interface.go`

Add VectorSearch method to ObjectRepository interface:

```go
// Add to ObjectRepository interface
VectorSearch(ctx context.Context, vector []float32, filter ObjectFilter) ([]*KnowledgeObject, error)
```

---

### Task 7: Configuration

**Files:**
- Modify: `internal/config/config.go`

Add retrieval configuration:

```go
type RetrievalConfig struct {
    Method                string `yaml:"method" json:"method"` // "rag" or "llm"
    EnableSufficiencyCheck bool   `yaml:"enable_sufficiency_check" json:"enable_sufficiency_check"`
    LLMProfile            string `yaml:"llm_profile" json:"llm_profile"`
    
    Categories TierConfig `yaml:"categories" json:"categories"`
    Items      TierConfig `yaml:"items" json:"items"`
    Resources  TierConfig `yaml:"resources" json:"resources"`
}

type TierConfig struct {
    Enabled bool `yaml:"enabled" json:"enabled"`
    TopK    int  `yaml:"top_k" json:"top_k"`
}

// Default config
func DefaultRetrievalConfig() RetrievalConfig {
    return RetrievalConfig{
        Method:                "rag",
        EnableSufficiencyCheck: true,
        LLMProfile:            "default",
        Categories: TierConfig{Enabled: true, TopK: 10},
        Items:      TierConfig{Enabled: true, TopK: 20},
        Resources:  TierConfig{Enabled: true, TopK: 5},
    }
}
```

---

### Task 8: API Integration

**Files:**
- Create: `internal/server/retrieve.go`

```go
package server

import (
    "encoding/json"
    "net/http"
    
    "github.com/ideacrafterslabs/ctxt/internal/retrieval"
    "github.com/ideacrafterslabs/ctxt/internal/storage"
)

type RetrieveHandler struct {
    workflow *retrieval.Workflow
}

type RetrieveRequest struct {
    Query              string   `json:"query"`
    ConversationHistory []string `json:"conversation_history,omitempty"`
    Filter             map[string]any `json:"filter,omitempty"`
}

type RetrieveResponse struct {
    NeedsRetrieval bool     `json:"needs_retrieval"`
    OriginalQuery  string   `json:"original_query"`
    RewrittenQuery string   `json:"rewritten_query,omitempty"`
    NextStepQuery  string   `json:"next_step_query,omitempty"`
    Categories     []string `json:"categories,omitempty"`
    Items          []string `json:"items,omitempty"`
    Resources      []string `json:"resources,omitempty"`
}

func (h *RetrieveHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    var req RetrieveRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        http.Error(w, err.Error(), http.StatusBadRequest)
        return
    }
    
    filter := storage.ObjectFilter{}
    // Parse filter from request...
    
    result, err := h.workflow.Retrieve(r.Context(), req.Query, req.ConversationHistory, filter)
    if err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }
    
    resp := RetrieveResponse{
        NeedsRetrieval: result.NeedsRetrieval,
        OriginalQuery:  result.OriginalQuery,
        RewrittenQuery: result.RewrittenQuery,
        NextStepQuery:  result.NextStepQuery,
    }
    
    for _, cat := range result.Categories {
        resp.Categories = append(resp.Categories, cat.ID)
    }
    for _, item := range result.Items {
        resp.Items = append(resp.Items, item.ID)
    }
    for _, res := range result.Resources {
        resp.Resources = append(resp.Resources, res.ID)
    }
    
    json.NewEncoder(w).Encode(resp)
}
```

---

## Testing Strategy

### Unit Tests

1. **Sufficiency Checker Tests** (`sufficiency_test.go`)
   - Test decision extraction from various LLM response formats
   - Test query rewriting extraction
   - Test fallback behavior on malformed responses

2. **Workflow Tests** (`workflow_test.go`)
   - Test early termination after categories
   - Test progression through all tiers
   - Test query evolution through tiers

3. **Retriever Tests** (`retriever_test.go`)
   - Mock vector search responses
   - Test tier-specific filtering

### Integration Tests

```go
func TestProgressiveRetrieval_E2E(t *testing.T) {
    // 1. Seed test data
    // 2. Create workflow with real storage
    // 3. Execute queries
    // 4. Verify early termination when appropriate
}
```

---

## Configuration Example

```yaml
retrieval:
  method: rag
  enable_sufficiency_check: true
  llm_profile: default
  
  categories:
    enabled: true
    top_k: 10
    
  items:
    enabled: true
    top_k: 20
    
  resources:
    enabled: true
    top_k: 5
```

---

## Performance Expectations

| Scenario | Without Sufficiency | With Sufficiency | Savings |
|----------|---------------------|------------------|---------|
| Simple query (answered by categories) | 3 retrievals + 3 checks | 1 retrieval + 1 check | ~60% |
| Medium query (needs items) | 3 retrievals + 3 checks | 2 retrievals + 2 checks | ~30% |
| Complex query (needs resources) | 3 retrievals + 3 checks | 3 retrievals + 3 checks | 0% |

---

## Future Enhancements

1. **Parallel Tier Fetching** - Fetch all tiers in parallel, use sufficiency check to prune
2. **Adaptive TopK** - Adjust top_k based on sufficiency confidence
3. **Caching** - Cache sufficiency decisions for similar queries
4. **Multi-Query** - Split complex queries into sub-queries with separate retrieval

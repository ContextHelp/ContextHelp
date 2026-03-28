# Persona: Platform Integrators

**Primary Role:** Third-party developers extending dPKMS + `ctxt` with custom implementations

---

## Goals

- Extend dPKMS with custom storage backends, encryption providers, query operators
- Implement custom AI providers for enrichment and NLQ normalization
- Build tailored capture and composition UI layers (web, mobile, IDE plugins)
- Integrate with external knowledge sources (industry registries, APIs, data lakes)
- Improve extraction quality for difficult websites and feed-linked articles with domain-specific rules
- Monetize or white-label the system for specific domains or use cases

---

## Interaction Pattern

### Plugin Development Workflow

#### 1. Choose Plugin Type
- **Pipeline Extension** — Custom enrichment step (e.g., "extract code metrics", "detect toxicity", "extract product specs")
- **Storage Backend** — Alternative to SQLite (e.g., Postgres, DynamoDB, MongoDB)
- **Registry Adapter** — Connect to external knowledge sources (e.g., "GitHub repository registry", "ArXiv paper registry")
- **Query Operator** — Extend RSQL with domain operators (e.g., `similar_code==`, `architecture_layer==`)
- **AI Provider** — Custom LLM backend or constrained generation library (e.g., LMQL plugin, specialized encoder)
- **Background Observer** — Monitor system events (e.g., screen watcher, clipboard monitor, Git activity)
- **Translation Provider** — Custom language translation (e.g., industry terminology translator)
- **Ranking Algorithm** — Custom result reranking (e.g., domain-expertise weighting, time-decay)

#### 2. Implement Required Interfaces
```go
// Example: Pipeline Extension Plugin
func (p *MyPlugin) Register(ctx HostContext) error {
  return ctx.RegisterPipelineStep(&MyEnrichmentStep{})
}

type MyEnrichmentStep struct{}

func (s *MyEnrichmentStep) Name() string { return "extract-code-metrics" }

func (s *MyEnrichmentStep) Execute(ctx ExecutionContext, input interface{}) (interface{}, error) {
  // Structured extraction with hard output guarantees
  // Return JSON directly; no post-hoc parsing
  return map[string]interface{}{
    "complexity": 3,
    "lines_of_code": 124,
    "cyclomatic": 4,
  }, nil
}

func (s *MyEnrichmentStep) Retryable() bool { return true }
```

#### 3. Declare Permissions
```yaml
apiVersion: dpkms/v1
kind: Plugin
metadata:
  name: code-metrics-extractor
  version: 0.1.0
spec:
  pluginPath: ./plugins/code-metrics.so
  permissions:
    # Declare what this plugin needs
    subprocess: true          # Can run external CLI tools
    network: false            # No network access
    filesystem: read          # Can only read files
    clipboard: false          # No clipboard access
    screen_capture: false     # No screen access
    modify_bookmarks: false   # Cannot modify objects
  configSchema:
    type: object
    properties:
      type: { const: "code-metrics" }
      language: { enum: ["go", "python", "typescript"] }
      threshold_complexity: { type: integer, minimum: 1 }
```

#### 4. Extend Config Schema (Optional)
```yaml
# In main configuration.yaml, if integrator adds new provider type:
myPlugin:
  type: code-metrics           # Custom type value
  language: python
  threshold_complexity: 5
  # More custom properties...
```

#### 5. Test in Isolation
```bash
# Load plugin without full system
dpkms --plugin ./plugins/code-metrics.so --no-enrichment

# Test pipeline step directly
dpkms test-step --plugin code-metrics --input example.py

# Integration test with real storage
dpkms --plugin code-metrics --storage sqlite:test.db analyze example.py
```

### Integration Points

#### AI Provider Plugin Example
```go
// Implement AI provider interface for constrained extraction
type LMQLProvider struct {
  config Config
}

func (p *LMQLProvider) Call(ctx context.Context, prompt string,
    constraints Constraints) (ConstrainedOutput, error) {
  // Execute LMQL with token-level constraints
  // Return structured output (intent, entities, decisions)
  // No parsing errors possible (hard constraints enforced)
}

// Register as AI provider
func (p *Plugin) Register(ctx HostContext) error {
  return ctx.RegisterAIProvider("lmql", p.lmqlProvider)
}

// Then use in config:
// aiProvider:
//   type: lmql
//   backend: local
//   model: llama3
```

#### Registry Adapter Plugin Example
```go
// Implement registry connector for external knowledge source
type GitHubRepoRegistry struct {
  token string
  org   string
}

func (r *GitHubRepoRegistry) Search(ctx context.Context,
    query string) ([]RegistryResult, error) {
  // Scatter: query GitHub repos, issues, discussions
  // Return results with canonical IDs
}

func (r *GitHubRepoRegistry) GetMetadata(ctx context.Context,
    id string) (RegistryMetadata, error) {
  // Get full details (stars, contributors, language, topics)
}

// Register as registry
func (p *Plugin) Register(ctx HostContext) error {
  return ctx.RegisterRegistry("github-org", r)
}

// Then use in config:
// registries:
//   - name: github-org
//     type: github
//     org: mycompany
//     token: ${GITHUB_TOKEN}
```

#### Custom Ranking Algorithm Plugin Example
```go
// Implement reranking for domain-specific relevance
type DomainExpertiseRanker struct {
  expertise map[string]float64  // domain → weight
}

func (r *DomainExpertiseRanker) Rank(ctx context.Context,
    results []SearchResult) ([]SearchResult, error) {
  // Rerank by domain expertise + recency + popularity
  // Return reordered results with new scores
}

// Register as ranking method
func (p *Plugin) Register(ctx HostContext) error {
  return ctx.RegisterRankingMethod("domain-expertise", r)
}

// Then use in config:
// rankingMethod: domain-expertise
```

---

## Key Pain Points

- **Plugin Versioning:** Which core version is my plugin compatible with? Breaking changes in interfaces
- **Permission Model Friction:** Too strict (plugin blocked at runtime), too loose (security risk)
- **Documentation Lag:** Interface changes faster than plugin development guides
- **Testing Isolation:** Hard to test plugin without full system (storage, jobs, registries)
- **Deployment Friction:** Getting users to approve plugin installation (permissions, code review)
- **Monetization:** How do I charge for a plugin? License key management?
- **White-Labeling:** How do I customize UI/branding for a specific customer?

---

## System Leverage

### Capability Scoping
- Plugins declare required permissions upfront
- Host rejects calls exceeding declared capabilities
- Users can inspect plugin permissions before installation
- Reduces blast radius of malicious or buggy plugins

### Plugin Interfaces
- Well-defined interfaces for each plugin type
- Versioned contracts (v1, v2) support backwards compatibility
- Type-safe (Go/TypeScript) prevents runtime surprises

### Polymorphic Config System
- Register custom config schema for new `type:` values
- Config loader auto-dispatches on type key
- Environment variable overrides for deployment flexibility

### Extraction Rule Maintenance
- Integrators can ship curated scraper rules for domains their users rely on
- The same rule set can improve browser capture, authenticated fetch, and feed enrichment together
- Rule-hit and fallback metadata make it easier to validate extraction quality in production

### Event-Driven Job Queue
- Plugins hook into ingestion + enrichment pipeline
- Can observe and react to job state changes (pending → running → completed)
- Plugins add enrichment steps to existing pipelines without modifying core

### Pluggable Everything
- Storage backends: implement `ObjectStore`, `JobStore`, `GraphStore`, `VectorStore` interfaces
- Encryption providers: implement key management, at-rest encryption
- Query operators: add new RSQL operators (e.g., `similar_code==`, `architecture_layer==`)
- Enrichment steps: implement extraction logic (tag assignment, entity extraction)
- Ranking algorithms: implement custom reranking strategies
- Translation providers: implement language detection and translation

### Registry Protocol
- Well-defined registry API (`/search`, `/metadata`, `/tags`, `/vocabulary`, `/embeddings`)
- Conflict resolution rules for federated searches
- Weighting and domain specificity scoring for result merging
- No schema coupling (registry schema independent of core schema)

---

## User Stories

Platform integrators interact with the system through these key stories:

### Platform Capture Integration
- [US-0202](../stories/capture/US-0202-github-capture.md) — GitHub Capture (API integration example)

### Extension Development
- [US-0300](../stories/ingestion/US-0300-importer-extension-interface.md) — Importer Extension Interface (build custom importers)

### Constraint & AI Integration
- [US-0014](../stories/enrichment/US-0014-constrain-extraction-with-lmql.md) — Constrain Extraction with LMQL (understand token-level constraints)
- [US-0050](../stories/enrichment/US-0050-extract-code-metrics-and-complexity.md) — Extract Code Metrics and Complexity (domain-specific extraction)

### Enrichment Plugins
- [US-0013](../stories/enrichment/US-0013-detect-and-extract-code-snippets.md) — Detect and Extract Code Snippets (custom enrichment example)
- [US-0042](../stories/plugins/US-0042-implement-custom-enrichment-plugin.md) — Implement Custom Enrichment Plugin (plugin framework)

### AI Provider Integration
- [US-0043](../stories/plugins/US-0043-implement-custom-ai-provider-plugin.md) — Implement Custom AI Provider Plugin (LMQL, instructor, outlines)

### Pipeline & Configuration
- [US-0028](../stories/admin/US-0028-register-custom-pipeline.md) — Register Custom Pipeline (custom processing workflows)
- [US-0029](../stories/admin/US-0029-install-and-enable-plugin.md) — Install and Enable Plugin (deployment & permissions)
- [US-0114](../stories/pipelines/US-0114-configure-domain-scraper-rules.md) — Configure Domain Scraper Rules (config-driven extraction quality)
- [US-0041](../stories/agents/US-0041-agent-uses-constrained-enrichment.md) — Agent Uses Constrained Enrichment (constraint patterns)

### Registry & Ranking
- [US-0044](../stories/plugins/US-0044-implement-registry-adapter-plugin.md) — Implement Registry Adapter Plugin (external knowledge sources)
- [US-0045](../stories/plugins/US-0045-implement-custom-ranking-algorithm.md) — Implement Custom Ranking Algorithm (relevance algorithms)

---

## Success Metrics

- **Plugin adoption:** Number of publicly listed plugins, download counts, active installations
- **Plugin reliability:** Plugin crash rate, permissions denied rate, user complaints
- **Time to develop:** Hours to build and ship a basic plugin (target: <4 hours for simple extension)
- **Monetization:** Revenue from premium plugins, subscription models
- **White-label adoption:** # of customers using customized versions

---

## Collaboration with Other Personas

- **Maintainers:** Integrators submit plugin PRs, request interface additions, report compatibility issues
- **Agents/LLMs:** Agents may leverage custom AI providers and ranking algorithms
- **Knowledge Workers:** Workers benefit from domain-specific plugins (e.g., "code extraction", "design analysis")
- **Operations:** Operations need clear deployment guidance for plugin management and updates

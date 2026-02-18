# Enrichment Provider Contract

**Status:** Draft
**Sprint:** 000 (Shared Kernel)
**Related:** `CROSS-PACKAGE-CONTRACTS.md`, `docs/ctxt/pipelines.md`, `docs/plugins/plugins-api.md`

---

## Purpose

This document defines the **Enrichment Provider Contract** — a stable interface enabling multiple implementations of the core transformation from unstructured content to structured knowledge objects.

The contract ensures:
- **Provider independence** — switch between BAML, direct LLM APIs, DSPy, Instructor, or custom models
- **User sovereignty** — users choose providers based on cost, privacy, performance, or compliance needs
- **Future-proofing** — adopt better enrichment technologies without breaking existing pipelines
- **Plugin extensibility** — third-party providers can implement the contract without modifying core

---

## Architecture Position

```
┌─────────────────────────────────────────────────────────────┐
│ ctxt Pipeline Engine                                        │
│ • Content routing                                           │
│ • Profile selection                                         │
│ • Job enqueueing                                            │
└──────────────────────┬──────────────────────────────────────┘
                       ↓
┌─────────────────────────────────────────────────────────────┐
│ Enrichment Provider Contract (dPKMS trait)                  │
│                                                              │
│ pub trait EnrichmentProvider {                              │
│   async fn enrich(request) -> Result<KnowledgeObject>       │
│   fn capabilities() -> ProviderCapabilities                 │
│   fn estimate_cost(request) -> Cost                         │
│ }                                                            │
└──────────────────────┬──────────────────────────────────────┘
                       ↓
        ┌──────────────┴──────────────┬──────────────┬─────────────┐
        ↓                             ↓              ↓             ↓
┌───────────────┐  ┌──────────────┐  ┌─────────┐  ┌──────────────┐
│ BAML Provider │  │ Direct LLM   │  │ Hybrid  │  │ Plugin-Based │
│ (reference)   │  │ Provider     │  │ Provider│  │ Providers    │
└───────────────┘  └──────────────┘  └─────────┘  └──────────────┘
                       ↓
┌─────────────────────────────────────────────────────────────┐
│ dPKMS Post-Processing                                       │
│ • Entity resolution (graph lookup)                          │
│ • Graph edge creation                                       │
│ • Embeddings generation                                     │
│ • Storage (transactional)                                   │
└─────────────────────────────────────────────────────────────┘
```

**Layer ownership:**
- **dPKMS**: Defines and enforces the contract (trait definition)
- **ctxt**: Provides reference implementations (BAML, Direct LLM)
- **Plugins**: Community implementations (local models, specialized extractors)

---

## Core Types

### EnrichmentProvider Trait

```rust
/// Core trait for all enrichment providers
#[async_trait]
pub trait EnrichmentProvider: Send + Sync {
    /// Provider identifier (e.g., "baml-0.1.2", "direct-anthropic", "local-llama")
    fn id(&self) -> &str;

    /// Extract structured knowledge from unstructured content
    async fn enrich(
        &self,
        request: EnrichmentRequest,
    ) -> Result<KnowledgeObject, EnrichmentError>;

    /// Describe provider capabilities
    fn capabilities(&self) -> ProviderCapabilities;

    /// Estimate cost before execution (for transparency and budget controls)
    fn estimate_cost(&self, request: &EnrichmentRequest) -> Cost;

    /// Optional: Streaming enrichment for large content
    async fn enrich_stream(
        &self,
        request: EnrichmentRequest,
    ) -> Result<impl Stream<Item = PartialKnowledgeObject>, EnrichmentError> {
        Err(EnrichmentError::StreamingNotSupported)
    }

    /// Optional: Batch enrichment for efficiency
    async fn enrich_batch(
        &self,
        requests: Vec<EnrichmentRequest>,
    ) -> Result<Vec<KnowledgeObject>, EnrichmentError> {
        let mut results = Vec::new();
        for request in requests {
            results.push(self.enrich(request).await?);
        }
        Ok(results)
    }
}
```

### EnrichmentRequest

```rust
/// Input to enrichment process
pub struct EnrichmentRequest {
    /// Raw unstructured content
    pub content: UnstructuredContent,

    /// Target schema version for output
    pub target_schema: SchemaVersion,

    /// Execution context and constraints
    pub context: EnrichmentContext,

    /// User's focus profile (Founder, Engineer, Research, etc.)
    pub profile: FocusProfile,

    /// Registry hints for entity resolution and taxonomy
    pub registry_hints: Vec<RegistryHint>,

    /// Optional: Previously extracted partial data (for re-enrichment)
    pub prior_extraction: Option<PartialKnowledgeObject>,
}

/// Unstructured input content
pub struct UnstructuredContent {
    pub format: ContentFormat,
    pub data: ContentData,
    pub metadata: ContentMetadata,
}

pub enum ContentFormat {
    PlainText,
    Markdown,
    HTML,
    PDF,
    Image { mime_type: String },
    Audio { mime_type: String, transcript: Option<String> },
    Video { mime_type: String, transcript: Option<String> },
}

pub enum ContentData {
    Text(String),
    Binary(Vec<u8>),
    Url(String),
}

pub struct ContentMetadata {
    pub source_url: Option<String>,
    pub title: Option<String>,
    pub author: Option<String>,
    pub published_at: Option<DateTime<Utc>>,
    pub language: Option<String>,
    pub size_bytes: usize,
}
```

### EnrichmentContext

```rust
/// Constraints and preferences for enrichment execution
pub struct EnrichmentContext {
    /// User-configured preferences
    pub user_preferences: Preferences,

    /// Budget constraints (max cost per enrichment)
    pub budget_constraints: Option<BudgetLimit>,

    /// Minimum quality threshold (0.0-1.0)
    pub quality_threshold: f32,

    /// Maximum acceptable latency
    pub max_latency: Option<Duration>,

    /// Privacy mode (prefer local, no external API calls)
    pub privacy_mode: bool,

    /// Re-enrichment flag (refining existing extraction)
    pub is_re_enrichment: bool,
}

pub struct BudgetLimit {
    pub max_cost_usd: f64,
    pub max_tokens: Option<usize>,
}
```

### KnowledgeObject (Output)

```rust
/// Structured output from enrichment (conforms to docs/ctxt/schema-object.md)
pub struct KnowledgeObject {
    /// Core content
    pub summary: String,
    pub atomic_notes: Vec<AtomicNote>,
    pub sections: Vec<Section>,

    /// Semantic annotations
    pub tags: Vec<Tag>,
    pub mentions: Vec<Mention>,
    pub entities: Vec<Entity>,

    /// Actionable extractions
    pub decisions: Vec<Decision>,
    pub tasks: Vec<Task>,
    pub questions: Vec<Question>,

    /// Metadata and provenance
    pub metadata: ObjectMetadata,
    pub provenance: EnrichmentProvenance,

    /// Quality signals
    pub confidence: ConfidenceScores,
}

pub struct AtomicNote {
    pub content: String,
    pub source_location: SourceLocation,
    pub confidence: f32,
    pub tags: Vec<String>,
}

pub struct Decision {
    pub what_was_decided: String,
    pub rationale: Option<String>,
    pub impact: DecisionImpact,
    pub status: DecisionStatus,
    pub stakeholders: Vec<String>,
    pub timestamp: Option<DateTime<Utc>>,
    pub confidence: f32,
    pub alternatives_considered: Vec<String>,
}

pub enum DecisionImpact {
    Low,
    Medium,
    High,
}

pub enum DecisionStatus {
    Open,
    Resolved,
    Superseded,
}

pub struct Task {
    pub description: String,
    pub priority: Option<String>,
    pub assignee: Option<String>,
    pub due_date: Option<DateTime<Utc>>,
    pub confidence: f32,
    pub dependencies: Vec<String>,
    pub context: Option<String>,
}

pub struct Mention {
    pub text: String,
    pub mention_type: MentionType,
    pub namespace: String,
    pub slug: String,
    pub confidence: f32,
    pub canonical_name: Option<String>,
}

pub enum MentionType {
    Entity,       // @entity.foo
    Person,       // @person.jane-doe
    Concept,      // @concept.zero-trust
    Location,     // @location.san-francisco
    Product,      // @product.contexthelp
    Organization, // @organization.anthropic
    Event,        // @event.wwdc-2024
    Project,      // @project.mobile-redesign
    System,       // @system.backend-api
}
```

### EnrichmentProvenance

```rust
/// Track which provider and configuration enriched this object
pub struct EnrichmentProvenance {
    /// Provider identifier
    pub enriched_by: String,

    /// Provider version
    pub provider_version: String,

    /// Model used (if applicable)
    pub model: Option<String>,

    /// Timestamp
    pub enriched_at: DateTime<Utc>,

    /// Schema version used
    pub schema_version: SchemaVersion,

    /// Cost incurred
    pub cost: Option<Cost>,

    /// Tokens consumed (input + output)
    pub tokens: Option<TokenUsage>,

    /// Execution time
    pub latency: Duration,

    /// Can this object be re-enriched with these providers?
    pub compatible_providers: Vec<String>,
}

pub struct TokenUsage {
    pub input_tokens: usize,
    pub output_tokens: usize,
}
```

### ProviderCapabilities

```rust
/// Describes what a provider can do
pub struct ProviderCapabilities {
    /// Supports images, audio, video (not just text)
    pub supports_multimodal: bool,

    /// Maximum content size in bytes
    pub max_content_size: usize,

    /// Supported input formats
    pub supported_formats: Vec<ContentFormat>,

    /// Requires network connectivity
    pub requires_network: bool,

    /// Supports streaming output
    pub supports_streaming: bool,

    /// Supports batch processing
    pub supports_batching: bool,

    /// Cost model
    pub cost_model: CostModel,

    /// Quality characteristics
    pub quality_profile: QualityProfile,
}

pub enum CostModel {
    Free,
    PerToken { input_cost: f64, output_cost: f64 },
    PerRequest { cost: f64 },
    Hybrid { base: f64, per_token: f64 },
}

pub struct QualityProfile {
    /// Typical quality score (0.0-1.0)
    pub typical_quality: f32,

    /// Typical latency
    pub typical_latency: Duration,

    /// Reliability (uptime, success rate)
    pub reliability: f32,
}
```

### Error Handling

```rust
#[derive(Debug, thiserror::Error)]
pub enum EnrichmentError {
    #[error("Provider not available: {0}")]
    ProviderUnavailable(String),

    #[error("Content too large: {size} bytes (max: {max})")]
    ContentTooLarge { size: usize, max: usize },

    #[error("Unsupported format: {0:?}")]
    UnsupportedFormat(ContentFormat),

    #[error("Budget exceeded: {spent} (max: {limit})")]
    BudgetExceeded { spent: f64, limit: f64 },

    #[error("Quality below threshold: {quality} (min: {threshold})")]
    QualityTooLow { quality: f32, threshold: f32 },

    #[error("Timeout after {0:?}")]
    Timeout(Duration),

    #[error("Schema validation failed: {0}")]
    SchemaValidation(String),

    #[error("Provider error: {0}")]
    ProviderError(String),

    #[error("Network error: {0}")]
    NetworkError(#[from] reqwest::Error),

    #[error("Streaming not supported by this provider")]
    StreamingNotSupported,
}
```

---

## Provider Discovery and Selection

### Configuration-Based Selection

Users configure providers in `~/.config/contexthelp/config.toml`:

```toml
[enrichment]
# Default provider for all pipelines
default_provider = "baml"

# Provider-specific configurations
[enrichment.providers.baml]
type = "baml"
schema_path = "~/.config/contexthelp/schemas/knowledge_object.baml"
default_client = "anthropic"
fallback_client = "openai"

[enrichment.providers.direct]
type = "llm_api"
api_key_env = "ANTHROPIC_API_KEY"
model = "claude-sonnet-4"
prompt_template = "~/.config/contexthelp/prompts/extraction.jinja2"

[enrichment.providers.local]
type = "ollama"
model = "llama3.2"
endpoint = "http://localhost:11434"

[enrichment.providers.hybrid]
type = "hybrid"
local_provider = "local"
cloud_provider = "baml"
routing_policy = "smart"  # Route based on content size, privacy mode, etc.

# Routing rules
[enrichment.routing]
# Route based on content characteristics
text_under_10kb = "direct"
multimodal = "baml"
privacy_mode_enabled = "local"
budget_constrained = "local"

# Per-pipeline overrides
[enrichment.pipeline_overrides]
text = "direct"
url = "baml"
image = "baml"
audio = "baml"
video = "baml"
```

### Runtime Selection

```rust
/// Provider registry manages available providers
pub struct ProviderRegistry {
    providers: HashMap<String, Box<dyn EnrichmentProvider>>,
    default_provider: String,
    routing_policy: RoutingPolicy,
}

impl ProviderRegistry {
    /// Select provider based on request characteristics
    pub fn select_provider(
        &self,
        request: &EnrichmentRequest,
    ) -> Result<&dyn EnrichmentProvider, EnrichmentError> {
        // Apply routing policy
        let provider_id = self.routing_policy.route(request);

        self.providers.get(&provider_id)
            .map(|p| p.as_ref())
            .ok_or_else(|| EnrichmentError::ProviderUnavailable(provider_id))
    }
}

pub enum RoutingPolicy {
    /// Always use default
    Default,

    /// Route based on content size
    BySizeThreshold { threshold: usize, small: String, large: String },

    /// Route based on privacy mode
    ByPrivacy { local: String, cloud: String },

    /// Route based on cost
    ByCost { budget: f64, cheap: String, expensive: String },

    /// Custom routing function (plugin-provided)
    Custom(Box<dyn Fn(&EnrichmentRequest) -> String>),
}
```

### CLI Override

```bash
# Use specific provider for this invocation
ctxt analyze --provider=local document.pdf

# Compare providers (enriches with both, shows differences)
ctxt analyze --provider=baml,direct --compare document.txt

# Set budget constraint
ctxt analyze --max-cost=0.10 document.pdf

# Force privacy mode (local-only providers)
ctxt analyze --privacy document.pdf
```

---

## Reference Implementations

### 1. BAML Provider (Reference Implementation)

```rust
/// Reference implementation using BAML
pub struct BamlEnrichmentProvider {
    runtime: BamlRuntime,
    config: BamlConfig,
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
        // Convert request to BAML input
        let baml_input = self.prepare_baml_input(&request)?;

        // Execute BAML function
        let baml_output = self.runtime
            .enrich_content(baml_input)
            .await
            .map_err(|e| EnrichmentError::ProviderError(e.to_string()))?;

        // Convert BAML output to KnowledgeObject
        let knowledge_object = self.parse_baml_output(baml_output)?;

        Ok(knowledge_object)
    }

    fn capabilities(&self) -> ProviderCapabilities {
        ProviderCapabilities {
            supports_multimodal: true,
            max_content_size: 1_000_000, // 1MB
            supported_formats: vec![
                ContentFormat::PlainText,
                ContentFormat::Markdown,
                ContentFormat::HTML,
            ],
            requires_network: true,
            supports_streaming: false,
            supports_batching: true,
            cost_model: CostModel::PerToken {
                input_cost: 0.000003,
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
        // Estimate tokens based on content size
        let estimated_input_tokens = request.content.size_bytes / 4; // rough estimate
        let estimated_output_tokens = 2000; // typical structured output size

        Cost {
            usd: (estimated_input_tokens as f64 * 0.000003) +
                 (estimated_output_tokens as f64 * 0.000015),
            tokens: Some(TokenUsage {
                input_tokens: estimated_input_tokens,
                output_tokens: estimated_output_tokens,
            }),
        }
    }
}
```

**BAML Schema** (in `~/.config/contexthelp/schemas/knowledge_object.baml`):

```baml
class KnowledgeObject {
  summary string @description("2-3 sentence summary capturing the essence of the content")
  atomic_notes AtomicNote[] @description("Discrete facts, ideas, and insights extracted from content")
  sections Section[] @description("Logical sections if content has clear structure")
  tags Tag[] @description("Semantic tags from taxonomies or inferred")
  mentions Mention[] @description("Entity mentions in @namespace.slug format")
  entities Entity[] @description("Resolved canonical entities")
  decisions Decision[] @description("Decisions made or discussed")
  tasks Task[] @description("Actionable items and next steps")
  questions Question[] @description("Open questions or areas needing clarification")
  confidence ConfidenceScores @description("Quality and confidence metrics")
}

class AtomicNote {
  content string @description("The atomic note content (single discrete idea)")
  source_location string? @description("Where in source this came from (paragraph, timestamp, etc)")
  confidence float @description("Confidence in extraction (0.0-1.0)")
  tags string[] @description("Tags specific to this note")
}

class Section {
  title string @description("Section title or heading")
  content string @description("Section content")
  level int @description("Heading level (1-6)")
  notes AtomicNote[] @description("Atomic notes extracted from this section")
}

class Tag {
  label string @description("Tag label")
  namespace string? @description("Taxonomy namespace if from registry")
  confidence float @description("Confidence in tag assignment (0.0-1.0)")
  weight float? @description("Importance weight if applicable")
}

class Mention {
  text string @description("Original mention text as it appears")
  mention_type MentionType @description("Type of entity mentioned")
  namespace string @description("Entity namespace")
  slug string @description("Entity slug (kebab-case)")
  confidence float @description("Confidence in mention extraction (0.0-1.0)")
  canonical_name string? @description("Canonical entity name if resolved")
}

enum MentionType {
  Entity
  Person
  Concept
  Location
  Product
  Organization
  Event
  Project
  System
}

enum DecisionImpact {
  LOW
  MEDIUM
  HIGH
}

enum DecisionStatus {
  OPEN
  RESOLVED
  SUPERSEDED
}

class Entity {
  id string @description("Stable entity ID")
  name string @description("Canonical entity name")
  entity_type string @description("Entity type (person, organization, concept, etc)")
  aliases string[] @description("Known aliases for this entity")
  description string? @description("Brief entity description")
  confidence float @description("Confidence in entity resolution (0.0-1.0)")
}

class Decision {
  what_was_decided string @description("Clear statement of the decision")
  rationale string? @description("Why this decision was made")
  impact DecisionImpact @description("Severity/scope of the decision")
  status DecisionStatus @description("Current lifecycle state of the decision")
  stakeholders string[] @description("People or entities involved in decision")
  timestamp string? @description("When decision was made (if mentioned)")
  confidence float @description("Confidence this is actually a decision (0.0-1.0)")
  alternatives_considered string[] @description("Other options that were considered")
}

class Task {
  description string @description("What needs to be done")
  priority string? @description("Priority level (high, medium, low, or P0-P4)")
  assignee string? @description("Who should do this (if mentioned)")
  due_date string? @description("Deadline or timeframe (if mentioned)")
  confidence float @description("Confidence this is an actionable task (0.0-1.0)")
  dependencies string[] @description("Other tasks this depends on")
  context string? @description("Additional context for completing the task")
}

class Question {
  question string @description("The open question")
  context string? @description("Context around why this question matters")
  potential_answers string[] @description("Possible answers if any are suggested")
  confidence float @description("Confidence this is a real question (0.0-1.0)")
}

class ConfidenceScores {
  overall float @description("Overall confidence in enrichment quality (0.0-1.0)")
  summary float @description("Confidence in summary quality (0.0-1.0)")
  extractions float @description("Confidence in structured extractions (0.0-1.0)")
  entity_resolution float @description("Confidence in entity resolution (0.0-1.0)")
}

function EnrichContent(
  content: string,
  content_type: string,
  profile: string,
  registry_hints: string[]
) -> KnowledgeObject {
  client Anthropic("claude-sonnet-4")
  prompt #"
    You are enriching content for ContextHelp, a knowledge management system.

    User profile: {{ profile }}
    Content type: {{ content_type }}

    Available taxonomies and entities from registries:
    {{ registry_hints }}

    Content to enrich:
    ---
    {{ content }}
    ---

    Extract ALL structured information:
    - Summary (2-3 sentences capturing essence)
    - Atomic notes (discrete facts, ideas, insights)
    - Sections (if applicable)
    - Tags (use registry taxonomies when possible)
    - Mentions (entities, people, concepts using @namespace.slug format)
    - Entities (resolved canonical entities with aliases)
    - Decisions (what was decided, rationale, impact, status, stakeholders)
    - Tasks (actionable items with priority, dependencies)
    - Questions (open questions or areas needing clarification)

    Return comprehensive knowledge object with confidence scores.
  "#
}
```

### 2. Direct LLM Provider (Simple Alternative)

```rust
/// Simple direct API implementation (validates swappability)
pub struct DirectLlmProvider {
    client: AnthropicClient,
    prompt_template: PromptTemplate,
}

#[async_trait]
impl EnrichmentProvider for DirectLlmProvider {
    fn id(&self) -> &str {
        "direct-anthropic-1.0"
    }

    async fn enrich(
        &self,
        request: EnrichmentRequest,
    ) -> Result<KnowledgeObject, EnrichmentError> {
        // Render prompt from template
        let prompt = self.prompt_template.render(&request)?;

        // Call Anthropic API with structured output
        let response = self.client
            .messages()
            .create(MessageRequest {
                model: "claude-sonnet-4".to_string(),
                messages: vec![Message::user(prompt)],
                // Use native structured output when available
                response_format: Some(ResponseFormat::JsonSchema {
                    schema: KNOWLEDGE_OBJECT_JSON_SCHEMA.clone(),
                }),
                ..Default::default()
            })
            .await
            .map_err(|e| EnrichmentError::ProviderError(e.to_string()))?;

        // Parse structured response
        let knowledge_object = serde_json::from_str(&response.content[0].text)
            .map_err(|e| EnrichmentError::SchemaValidation(e.to_string()))?;

        Ok(knowledge_object)
    }

    fn capabilities(&self) -> ProviderCapabilities {
        ProviderCapabilities {
            supports_multimodal: false,
            max_content_size: 800_000,
            supported_formats: vec![ContentFormat::PlainText],
            requires_network: true,
            supports_streaming: true,
            supports_batching: false,
            cost_model: CostModel::PerToken {
                input_cost: 0.000003,
                output_cost: 0.000015,
            },
            quality_profile: QualityProfile {
                typical_quality: 0.80,
                typical_latency: Duration::from_secs(3),
                reliability: 0.99,
            },
        }
    }

    fn estimate_cost(&self, request: &EnrichmentRequest) -> Cost {
        // Similar to BAML implementation
        todo!()
    }
}
```

### 3. Hybrid Provider (Local + Cloud)

```rust
/// Routes to local model first, falls back to cloud
pub struct HybridProvider {
    local: Box<dyn EnrichmentProvider>,
    cloud: Box<dyn EnrichmentProvider>,
    policy: HybridPolicy,
}

pub struct HybridPolicy {
    pub local_max_size: usize,
    pub quality_threshold: f32,
    pub always_local_if_privacy: bool,
}

#[async_trait]
impl EnrichmentProvider for HybridProvider {
    fn id(&self) -> &str {
        "hybrid-1.0"
    }

    async fn enrich(
        &self,
        request: EnrichmentRequest,
    ) -> Result<KnowledgeObject, EnrichmentError> {
        // Force local if privacy mode
        if request.context.privacy_mode && self.policy.always_local_if_privacy {
            return self.local.enrich(request).await;
        }

        // Try local first for small content
        if request.content.metadata.size_bytes <= self.policy.local_max_size {
            match self.local.enrich(request.clone()).await {
                Ok(result) if result.confidence.overall >= self.policy.quality_threshold => {
                    return Ok(result);
                }
                Ok(_low_quality) => {
                    // Fall through to cloud for better quality
                }
                Err(_) => {
                    // Fall through to cloud on error
                }
            }
        }

        // Use cloud for large content or when local quality insufficient
        self.cloud.enrich(request).await
    }

    fn capabilities(&self) -> ProviderCapabilities {
        // Report combined capabilities
        let local_caps = self.local.capabilities();
        let cloud_caps = self.cloud.capabilities();

        ProviderCapabilities {
            supports_multimodal: local_caps.supports_multimodal || cloud_caps.supports_multimodal,
            max_content_size: cloud_caps.max_content_size,
            supported_formats: cloud_caps.supported_formats,
            requires_network: false, // Can work offline (degraded)
            supports_streaming: cloud_caps.supports_streaming,
            supports_batching: local_caps.supports_batching && cloud_caps.supports_batching,
            cost_model: CostModel::Hybrid {
                base: 0.0, // Free local
                per_token: 0.000003, // Cloud fallback cost
            },
            quality_profile: cloud_caps.quality_profile, // Report cloud quality
        }
    }

    fn estimate_cost(&self, request: &EnrichmentRequest) -> Cost {
        // Estimate based on routing decision
        if request.content.metadata.size_bytes <= self.policy.local_max_size {
            Cost { usd: 0.0, tokens: None } // Assume local
        } else {
            self.cloud.estimate_cost(request) // Cloud
        }
    }
}
```

---

## Plugin Integration

### Plugin-Provided Providers

Third-party plugins can implement `EnrichmentProvider`:

```rust
// In plugin crate
use contexthelp_dpkms::enrichment::{EnrichmentProvider, EnrichmentRequest, KnowledgeObject};

pub struct CustomProvider {
    // Plugin-specific state
}

#[async_trait]
impl EnrichmentProvider for CustomProvider {
    fn id(&self) -> &str {
        "plugin-custom-extractor-1.0"
    }

    async fn enrich(&self, request: EnrichmentRequest) -> Result<KnowledgeObject, EnrichmentError> {
        // Custom implementation (e.g., domain-specific model, specialized parsing)
        todo!()
    }

    // ... implement other methods
}

// Plugin exports provider via plugin API
#[no_mangle]
pub extern "C" fn register_plugin(registry: &mut PluginRegistry) {
    registry.register_enrichment_provider(Box::new(CustomProvider::new()));
}
```

### Plugin Discovery

```toml
# In ~/.config/contexthelp/plugins.toml
[plugins.medical_extractor]
path = "~/.local/share/contexthelp/plugins/medical_extractor.so"
enabled = true

# Plugin provides its own enrichment provider
[plugins.medical_extractor.enrichment]
provider_id = "medical-extractor-1.0"
specializes_in = ["medical_notes", "clinical_reports"]
```

Users can then use plugin-provided providers:

```bash
ctxt analyze --provider=medical-extractor medical_report.pdf
```

---

## Migration and Compatibility

### Re-Enrichment Support

When providers improve or users switch, re-enrich existing objects:

```bash
# Re-enrich all objects enriched by old provider
ctxt re-enrich --from=baml-0.1.0 --to=baml-0.2.0

# Re-enrich low-confidence objects with better provider
ctxt re-enrich --where="confidence.overall < 0.7" --to=direct

# Re-enrich specific object
ctxt re-enrich --id=abc123 --to=local --privacy
```

### Provenance Tracking

Every `KnowledgeObject` includes full provenance:

```json
{
  "provenance": {
    "enriched_by": "baml-0.1.2",
    "provider_version": "0.1.2",
    "model": "claude-sonnet-4",
    "enriched_at": "2024-12-01T10:30:00Z",
    "schema_version": "v1",
    "cost": { "usd": 0.045 },
    "tokens": { "input_tokens": 8500, "output_tokens": 2200 },
    "latency": "4.2s",
    "compatible_providers": ["baml-0.2.0", "direct-anthropic-1.0"]
  }
}
```

### Schema Evolution

As `KnowledgeObject` schema evolves:

```rust
pub enum SchemaVersion {
    V1,  // Initial schema
    V2,  // Added `questions` field
    V3,  // Added `confidence.extractions` score
    V4,  // Added Decision.impact/status enums, MentionType.Project/System, expanded fields
}

impl EnrichmentProvider {
    /// Provider declares which schema versions it can produce
    fn supported_schemas(&self) -> Vec<SchemaVersion>;
}
```

Providers can support multiple schema versions, enabling gradual migration.

---

## Testing Strategy

### Contract Compliance Tests

All providers must pass contract compliance tests:

```rust
#[cfg(test)]
mod tests {
    use super::*;
    use contexthelp_testing::enrichment::*;

    #[tokio::test]
    async fn test_provider_compliance() {
        let provider = BamlEnrichmentProvider::new(test_config());

        // Standard compliance test suite
        assert_provider_implements_contract(&provider).await;
        assert_provider_handles_errors_correctly(&provider).await;
        assert_provider_respects_budget_limits(&provider).await;
        assert_provider_produces_valid_schema(&provider).await;
    }

    #[tokio::test]
    async fn test_capabilities_accurate() {
        let provider = BamlEnrichmentProvider::new(test_config());
        let caps = provider.capabilities();

        // Verify capabilities are accurate
        assert_eq!(caps.supports_multimodal, true);
        assert_max_content_size_enforced(&provider, caps.max_content_size).await;
    }
}
```

### Provider Comparison Tests

```rust
#[tokio::test]
async fn test_provider_equivalence() {
    let baml = BamlEnrichmentProvider::new(test_config());
    let direct = DirectLlmProvider::new(test_config());

    let test_content = load_test_content("sample_article.txt");
    let request = EnrichmentRequest::new(test_content);

    let baml_result = baml.enrich(request.clone()).await.unwrap();
    let direct_result = direct.enrich(request).await.unwrap();

    // Results should be semantically equivalent (not necessarily identical)
    assert_semantic_similarity(&baml_result, &direct_result) > 0.85;
}
```

---

## Performance Considerations

### Provider Selection Performance

- Provider registry lookup: O(1) hash map
- Routing policy evaluation: O(1) for simple policies, O(n) for complex rules
- Negligible overhead (<1ms) compared to enrichment latency (seconds)

### Caching

Providers can implement internal caching:

```rust
pub trait EnrichmentProvider {
    /// Optional: Check if result is cached
    async fn check_cache(&self, request: &EnrichmentRequest) -> Option<KnowledgeObject> {
        None
    }

    /// Optional: Store result in cache
    async fn cache_result(&self, request: &EnrichmentRequest, result: &KnowledgeObject) {
        // Default: no-op
    }
}
```

### Batching

Providers supporting batching can amortize network overhead:

```rust
// Process 10 articles in one batch (if provider supports it)
let requests = load_multiple_articles(10);
let results = provider.enrich_batch(requests).await?;
```

---

## Security and Privacy

### API Key Management

Providers access credentials via secure storage:

```rust
impl BamlEnrichmentProvider {
    fn new(config: BamlConfig) -> Self {
        let api_key = config.api_key_env
            .and_then(|env_var| std::env::var(env_var).ok())
            .or_else(|| keyring::get_api_key("anthropic"))
            .expect("API key not configured");

        // ... initialize client
    }
}
```

### Privacy Mode Enforcement

When `context.privacy_mode = true`:

```rust
impl ProviderRegistry {
    pub fn select_provider(&self, request: &EnrichmentRequest) -> Result<&dyn EnrichmentProvider> {
        let provider = self.routing_policy.route(request);

        // Enforce privacy mode
        if request.context.privacy_mode {
            let caps = provider.capabilities();
            if caps.requires_network {
                return Err(EnrichmentError::ProviderUnavailable(
                    "Privacy mode requires local-only provider".to_string()
                ));
            }
        }

        Ok(provider)
    }
}
```

### Data Retention

Providers must document data retention policies:

```rust
pub struct ProviderCapabilities {
    // ...
    pub data_retention: DataRetentionPolicy,
}

pub enum DataRetentionPolicy {
    /// No data leaves the device
    LocalOnly,

    /// Data sent to API but not retained
    ZeroRetention,

    /// Data may be retained for training (user must opt in)
    MayRetainForTraining { opt_out_available: bool },
}
```

---

## Documentation Requirements

Each provider implementation must document:

1. **Capabilities** — what it can and cannot do
2. **Setup** — configuration, API keys, dependencies
3. **Cost model** — pricing structure and typical costs
4. **Quality characteristics** — typical quality scores, latency
5. **Privacy implications** — where data goes, retention policy
6. **Limitations** — known issues, content size limits
7. **Examples** — sample configurations and usage

Template: `docs/plugins/PROVIDER-TEMPLATE.md`

---

## Roadmap

### Sprint 0 (Foundation)
- [ ] Define `EnrichmentProvider` trait in dPKMS
- [ ] Define core types (Request, Response, Capabilities, Error)
- [ ] Document contract (this file)

### Sprint 1 (Reference Implementation)
- [ ] Implement BAML provider as reference
- [ ] Create compliance test suite
- [ ] Validate contract against real-world usage

### Sprint 2 (Prove Swappability)
- [ ] Implement Direct LLM provider
- [ ] Implement provider registry and selection
- [ ] Add CLI provider override (`--provider`)
- [ ] Document migration between providers

### Sprint 3 (Advanced Features)
- [ ] Implement hybrid provider (local + cloud)
- [ ] Add streaming support
- [ ] Add batch processing
- [ ] Cost tracking and budget enforcement

### Sprint 4 (Plugin Ecosystem)
- [ ] Document plugin enrichment provider API
- [ ] Create example plugin provider
- [ ] Add provider discovery and installation
- [ ] Provider marketplace/registry

### Future
- [ ] A/B testing framework (compare provider quality)
- [ ] Auto-tuning (learn optimal provider per content type)
- [ ] Federated providers (run enrichment on user's behalf)
- [ ] Privacy-preserving enrichment (homomorphic encryption)

---

## Open Questions

1. **Streaming protocol**: Should `enrich_stream()` emit partial updates or chunk-by-chunk results?

2. **Quality measurement**: How to objectively score enrichment quality for auto-routing?

3. **Provider dependencies**: Should providers declare dependencies (e.g., "requires ollama running")?

4. **Fallback chains**: Support automatic fallback (primary → secondary → tertiary)?

5. **Custom schemas**: Allow users to extend `KnowledgeObject` schema per-pipeline?

6. **Diff-based re-enrichment**: When re-enriching, only process changed sections?

---

## References

- `docs/ctxt/pipelines.md` — Pipeline architecture
- `docs/ctxt/schema-object.md` — Knowledge object schema
- `docs/plugins/plugins-api.md` — Plugin API contract
- `docs/dpkms/jobs-and-ingestion.md` — Job queue integration
- `CROSS-PACKAGE-CONTRACTS.md` — dPKMS ↔ ctxt integration

// BAML Schema for ContextHelp Knowledge Object
// This schema defines the structured output format for content enrichment
// Corresponds to: docs/ctxt/schema-object.md
//
// NOTE: metadata (ObjectMetadata) and provenance (EnrichmentProvenance) are
// added by the Go pipeline layer post-extraction, not by the LLM. The BAML
// schema represents only the LLM's structured extraction output. See
// ENRICHMENT-PROVIDER-CONTRACT.md and ENRICHMENT-GO-IMPLEMENTATION.md for
// the full KnowledgeObject with metadata + provenance fields.

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
  Entity @description("Generic entity (@entity.foo)")
  Person @description("Person (@person.jane-doe)")
  Concept @description("Concept or topic (@concept.zero-trust)")
  Location @description("Location or place (@location.san-francisco)")
  Product @description("Product or service (@product.contexthelp)")
  Organization @description("Company or organization (@organization.anthropic)")
  Event @description("Event or occurrence (@event.wwdc-2024)")
  Project @description("Initiative or project (@project.mobile-redesign)")
  System @description("Software system or platform (@system.backend-api)")
}

class Entity {
  id string @description("Stable entity ID")
  name string @description("Canonical entity name")
  entity_type string @description("Entity type (person, organization, concept, etc)")
  aliases string[] @description("Known aliases for this entity")
  description string? @description("Brief entity description")
  confidence float @description("Confidence in entity resolution (0.0-1.0)")
}

enum DecisionImpact {
  LOW @description("Low impact — minor or routine decision")
  MEDIUM @description("Medium impact — affects team or workflow")
  HIGH @description("High impact — strategic or architectural significance")
}

enum DecisionStatus {
  OPEN @description("Decision is still under discussion")
  RESOLVED @description("Decision has been made and finalized")
  SUPERSEDED @description("Decision was replaced by a later decision")
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

// Main enrichment function
function EnrichContent(
  content: string,
  content_type: string,
  profile: string,
  registry_hints: string[]
) -> KnowledgeObject {
  client Anthropic("claude-sonnet-4")
  prompt #"
    You are enriching content for ContextHelp, a personal knowledge management system.

    **User Context:**
    - Profile: {{ profile }}
    - Content type: {{ content_type }}

    {% if registry_hints and registry_hints|length > 0 %}
    **Available Taxonomies and Known Entities:**
    {% for hint in registry_hints %}
    - {{ hint }}
    {% endfor %}

    Use these when tagging and extracting mentions. Prefer existing entities over creating new ones.
    {% endif %}

    **Content to Enrich:**
    ---
    {{ content }}
    ---

    **Your Task:**
    Extract ALL structured information from this content:

    1. **Summary**: Write a clear 2-3 sentence summary that captures the essence

    2. **Atomic Notes**: Extract discrete facts, ideas, and insights
       - Each note should be self-contained and specific
       - Include source location when possible (e.g., "paragraph 3", "00:45 timestamp")
       - Assign confidence based on how clearly stated it is

    3. **Sections**: If content has clear structure (headings, chapters), preserve it

    4. **Tags**: Apply semantic tags
       - Use registry taxonomies when available
       - Infer appropriate tags when not in registry
       - Include confidence and weight

    5. **Mentions**: Extract entity mentions using @namespace.slug format
       - @person.jane-doe for people
       - @organization.anthropic for organizations
       - @concept.knowledge-graph for concepts
       - @product.contexthelp for products
       - @location.san-francisco for locations
       - @event.wwdc-2024 for events
       - @project.mobile-redesign for initiatives/projects
       - @system.backend-api for software systems/platforms

    6. **Entities**: Resolve mentions to canonical entities
       - Deduplicate (Jane, Jane Doe, J. Doe → same entity)
       - Provide aliases, type, description

    7. **Decisions**: Extract explicit decisions
       - What was decided?
       - Why? (rationale)
       - Impact level: LOW, MEDIUM, or HIGH
       - Status: OPEN, RESOLVED, or SUPERSEDED
       - Who was involved?
       - What alternatives were considered?

    8. **Tasks**: Extract actionable items
       - Clear description of what needs to be done
       - Priority, assignee, deadline if mentioned
       - Dependencies on other tasks

    9. **Questions**: Identify open questions or unclear areas
       - What's being asked?
       - Why does it matter?
       - Any potential answers suggested?

    10. **Confidence Scores**: Be honest about quality
        - Overall: How well does this enrichment capture the content?
        - Summary: How accurate is the summary?
        - Extractions: How complete are the structured extractions?
        - Entity Resolution: How confident are you in entity matching?

    **Quality Guidelines:**
    - Be comprehensive but precise
    - Don't hallucinate details not in the content
    - Use high confidence (0.8+) only for clear, explicit information
    - Use medium confidence (0.5-0.7) for inferred or implicit information
    - Use low confidence (<0.5) for uncertain extractions
    - When in doubt, extract it but mark confidence accordingly

    **Profile-Specific Focus:**
    {% if profile == "Founder" %}
    Pay special attention to: business decisions, metrics, competitive intelligence, market insights, strategic moves
    {% elif profile == "Engineer" %}
    Pay special attention to: technical decisions, architecture, dependencies, implementation details, code patterns
    {% elif profile == "Research" %}
    Pay special attention to: hypotheses, findings, methodology, citations, open questions, future work
    {% else %}
    Extract comprehensively across all dimensions
    {% endif %}
  "#
}

// Specialized function for multimodal content (images, audio transcripts, video)
function EnrichMultimodal(
  content: string,
  media_type: string,
  visual_description: string?,
  transcript: string?,
  profile: string,
  registry_hints: string[]
) -> KnowledgeObject {
  client Anthropic("claude-sonnet-4")
  prompt #"
    You are enriching {{ media_type }} content for ContextHelp.

    **User Profile:** {{ profile }}

    {% if visual_description %}
    **Visual Content Description:**
    {{ visual_description }}
    {% endif %}

    {% if transcript %}
    **Transcript/OCR:**
    {{ transcript }}
    {% endif %}

    {% if content %}
    **Additional Context:**
    {{ content }}
    {% endif %}

    {% if registry_hints and registry_hints|length > 0 %}
    **Known Entities:**
    {% for hint in registry_hints %}
    - {{ hint }}
    {% endfor %}
    {% endif %}

    Extract structured knowledge following the same guidelines as text enrichment.
    For visual content, describe key elements, text, diagrams, and their significance.
    For audio/video, note important timestamps for key insights.
  "#
}

// Lightweight enrichment for quick preview (low cost)
function EnrichPreview(
  content: string,
  max_length: int
) -> KnowledgeObject {
  client Anthropic("claude-haiku-3-5")
  prompt #"
    Provide a quick enrichment preview (lightweight extraction).

    Content (first {{ max_length }} chars):
    {{ content }}

    Extract:
    - Brief summary
    - Top 3-5 atomic notes
    - Most important mentions
    - Any clear decisions or tasks

    Keep it fast and cheap. Use medium-to-low confidence scores.
  "#
}

// Re-enrichment function (refines existing extraction)
function RefineEnrichment(
  original_content: string,
  prior_extraction: string,
  refinement_focus: string,
  profile: string
) -> KnowledgeObject {
  client Anthropic("claude-sonnet-4")
  prompt #"
    You are refining a previous knowledge extraction.

    **Original Content:**
    {{ original_content }}

    **Prior Extraction (JSON):**
    {{ prior_extraction }}

    **Refinement Focus:**
    {{ refinement_focus }}

    **User Profile:** {{ profile }}

    Improve the extraction by:
    {% if refinement_focus contains "quality" %}
    - Increasing confidence scores where appropriate
    - Adding missing details
    - Resolving ambiguous entities
    {% elif refinement_focus contains "decisions" %}
    - Extracting more decisions and their rationale
    - Identifying alternatives considered
    {% elif refinement_focus contains "tasks" %}
    - Extracting more actionable items
    - Clarifying priorities and dependencies
    {% elif refinement_focus contains "entities" %}
    - Better entity resolution and deduplication
    - More complete entity descriptions
    {% else %}
    - General quality improvement across all dimensions
    {% endif %}

    Preserve good extractions from prior version, enhance weak ones.
  "#
}

// Test function for validation
test EnrichSampleArticle {
  functions [EnrichContent]
  args {
    content "Anthropic announced Claude 3.5 Sonnet, their most intelligent model yet. The new model shows significant improvements in coding, analysis, and instruction following. It will be available via API and Claude.ai starting today. Pricing remains the same as Claude 3 Sonnet at $3 per million input tokens."
    content_type "text"
    profile "Engineer"
    registry_hints [
      "@organization.anthropic",
      "@product.claude",
      "@concept.large-language-model"
    ]
  }
}

test EnrichDecisionDocument {
  functions [EnrichContent]
  args {
    content "After evaluating three options (PostgreSQL, MongoDB, and DynamoDB), we decided to use PostgreSQL for the primary datastore. Rationale: We need strong ACID guarantees and complex query support. Sarah and Mike agreed. We'll revisit in Q3 if scaling becomes an issue. Action items: Mike to set up RDS instance by Friday, Sarah to design initial schema."
    content_type "text"
    profile "Founder"
    registry_hints ["@person.sarah", "@person.mike"]
  }
}

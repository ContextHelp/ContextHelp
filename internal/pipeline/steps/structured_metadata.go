package steps

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Valid content types for structured metadata classification.
var validContentTypes = map[string]bool{
	"observation": true,
	"task":        true,
	"idea":        true,
	"reference":   true,
	"person_note": true,
	"decision":    true,
	"question":    true,
}

// Valid source types.
var validSourceTypes = map[string]bool{
	"text":    true,
	"url":     true,
	"file":    true,
	"api":     true,
	"webhook": true,
}

// StructuredMetadata holds extracted metadata for a knowledge object.
type StructuredMetadata struct {
	Type           string   `json:"type"`
	Topics         []string `json:"topics"`
	People         []string `json:"people"`
	ActionItems    []string `json:"action_items"`
	DatesMentioned []string `json:"dates_mentioned"`
	SourceType     string   `json:"source_type"`
	Confidence     float64  `json:"confidence"`
}

const structuredMetadataPrompt = `You are a metadata extraction assistant. Given content, extract structured metadata as a single JSON object with these fields:
- type: one of "observation", "task", "idea", "reference", "person_note", "decision", "question"
- topics: []string — relevant topic labels (0-10 items, lowercase)
- people: []string — people mentioned, as @person.slug format where possible (0-10 items)
- action_items: []string — explicit action items or todos (0-10 items)
- dates_mentioned: []string — dates in YYYY-MM-DD format (0-10 items)
- source_type: one of "text", "url", "file", "api", "webhook"
- confidence: float 0.0-1.0 indicating extraction confidence

Return ONLY valid JSON. Example:
{"type":"task","topics":["auth","security"],"people":["@person.alice"],"action_items":["review PR #42"],"dates_mentioned":["2026-05-01"],"source_type":"text","confidence":0.85}

Content:
`

// StructuredMetadataExtractor extracts structured metadata from content
// using an LLM and stores it under Metadata["enrichment.structured_metadata"].
type StructuredMetadataExtractor struct {
	pipeline.BaseContract
	llm    providers.LLMProvider
	schema *config.ProfileSchema
}

func NewStructuredMetadataExtractor() *StructuredMetadataExtractor {
	return &StructuredMetadataExtractor{
		BaseContract: pipeline.NewBaseContract(pipeline.StepContract{
			Requires:     []string{"RawContent"},
			Produces:     []string{"Metadata"},
			Capabilities: []string{"llm"},
		}),
	}
}

// NewStructuredMetadataExtractorWithLLM creates an extractor with an LLM provider.
func NewStructuredMetadataExtractorWithLLM(llm providers.LLMProvider) *StructuredMetadataExtractor {
	s := NewStructuredMetadataExtractor()
	s.llm = llm
	return s
}

// NewStructuredMetadataExtractorWithSchema creates an extractor constrained
// by a profile schema's entity types, topic vocabulary, and classification rules.
func NewStructuredMetadataExtractorWithSchema(
	llm providers.LLMProvider,
	schema *config.ProfileSchema,
) *StructuredMetadataExtractor {
	s := NewStructuredMetadataExtractorWithLLM(llm)
	s.schema = schema
	return s
}

func (s *StructuredMetadataExtractor) Name() string { return "structured_metadata" }

func (s *StructuredMetadataExtractor) Run(
	ctx context.Context,
	draft *storage.KnowledgeObject,
) (*storage.KnowledgeObject, error) {
	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}

	if s.llm == nil {
		draft.Metadata["metadata_status"] = "pending"
		return draft, nil
	}

	content := strings.TrimSpace(draft.RawContent)
	if content == "" {
		draft.Metadata["metadata_status"] = "pending"
		return draft, nil
	}

	// Truncate to keep prompt within reasonable bounds.
	if len(content) > 4000 {
		content = content[:4000]
	}

	raw, err := s.llm.Generate(ctx, structuredMetadataPrompt+content)
	if err != nil {
		// Optional enrichment: record the miss as metadata_status=pending so
		// a later pass can retry, and let the object continue un-enriched
		// rather than failing ingestion on an unavailable LLM.
		draft.Metadata["metadata_status"] = "pending"
		return draft, nil //nolint:nilerr // optional enrichment; recorded as metadata_status=pending for retry
	}

	meta, err := parseStructuredMetadata(raw)
	if err != nil {
		// Unparseable LLM output is an expected outcome, not a fault; marked
		// pending for retry on the same terms as a generation failure.
		draft.Metadata["metadata_status"] = "pending"
		return draft, nil //nolint:nilerr // optional enrichment; recorded as metadata_status=pending for retry
	}

	// Infer source_type from object if LLM returned empty/invalid.
	if !validSourceTypes[meta.SourceType] {
		meta.SourceType = inferSourceType(draft)
	}

	// Apply profile schema constraints when present.
	if s.schema != nil {
		applySchemaConstraints(meta, s.schema, content)
	}

	draft.Metadata["enrichment.structured_metadata"] = meta
	draft.Metadata["metadata_status"] = "complete"
	return draft, nil
}

// parseStructuredMetadata parses and validates LLM JSON response.
func parseStructuredMetadata(raw string) (*StructuredMetadata, error) {
	cleaned := strings.TrimSpace(raw)
	if idx := strings.Index(cleaned, "{"); idx > 0 {
		cleaned = cleaned[idx:]
	}
	if end := strings.LastIndex(cleaned, "}"); end >= 0 && end < len(cleaned)-1 {
		cleaned = cleaned[:end+1]
	}

	var meta StructuredMetadata
	if err := json.Unmarshal([]byte(cleaned), &meta); err != nil {
		return nil, err
	}

	// Constrain type to vocabulary.
	if !validContentTypes[meta.Type] {
		meta.Type = "observation"
	}

	// Constrain confidence.
	if meta.Confidence < 0 {
		meta.Confidence = 0
	}
	if meta.Confidence > 1 {
		meta.Confidence = 1
	}

	// Normalize nil slices to empty.
	if meta.Topics == nil {
		meta.Topics = []string{}
	}
	if meta.People == nil {
		meta.People = []string{}
	}
	if meta.ActionItems == nil {
		meta.ActionItems = []string{}
	}
	if meta.DatesMentioned == nil {
		meta.DatesMentioned = []string{}
	}

	return &meta, nil
}

// applySchemaConstraints constrains metadata to profile schema vocabulary.
// Classification rules are checked first (pattern match overrides LLM type).
// Entity types and topics are filtered to the profile's vocabulary.
func applySchemaConstraints(
	meta *StructuredMetadata,
	schema *config.ProfileSchema,
	content string,
) {
	// Classification rules: first match wins.
	for _, rule := range schema.ClassificationRules {
		re, err := regexp.Compile(rule.Pattern)
		if err != nil {
			continue
		}
		if re.MatchString(content) {
			meta.Type = rule.Type
			break
		}
	}

	// Constrain type to profile entity types (if defined).
	if len(schema.EntityTypes) > 0 {
		allowed := make(map[string]bool, len(schema.EntityTypes))
		for _, t := range schema.EntityTypes {
			allowed[t] = true
		}
		if !allowed[meta.Type] {
			meta.Type = schema.EntityTypes[0]
		}
	}

	// Constrain topics to profile vocabulary (if defined).
	if len(schema.TopicVocabulary) > 0 {
		allowed := make(map[string]bool, len(schema.TopicVocabulary))
		for _, t := range schema.TopicVocabulary {
			allowed[t] = true
		}
		filtered := meta.Topics[:0]
		for _, t := range meta.Topics {
			if allowed[t] {
				filtered = append(filtered, t)
			}
		}
		meta.Topics = filtered
	}
}

// inferSourceType derives source_type from the object's Source field.
func inferSourceType(draft *storage.KnowledgeObject) string {
	src := strings.ToLower(draft.Source)
	if strings.HasPrefix(src, "http://") || strings.HasPrefix(src, "https://") {
		return "url"
	}
	if strings.HasPrefix(src, "file://") || strings.HasSuffix(src, ".md") ||
		strings.HasSuffix(src, ".txt") || strings.HasSuffix(src, ".pdf") {
		return "file"
	}
	if strings.Contains(src, "api") || strings.Contains(src, "webhook") {
		if strings.Contains(src, "webhook") {
			return "webhook"
		}
		return "api"
	}
	return "text"
}

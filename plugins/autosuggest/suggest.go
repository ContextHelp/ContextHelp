package autosuggest

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// LLMProvider is the minimal interface needed from providers.LLMProvider.
// Defined locally so the plugin module does not need to import the full providers package.
type LLMProvider interface {
	Generate(ctx context.Context, prompt string) (string, error)
}

type suggestResponse struct {
	Tags     []string `json:"tags"`
	Mentions []string `json:"mentions"`
}

// SuggestTagsAndMentions calls the LLM to produce tag and @mention suggestions.
// Returns empty slices (not an error) if the LLM is nil or returns unparseable output.
func SuggestTagsAndMentions(ctx context.Context, obj *storage.KnowledgeObject, cfg AutoSuggestConfig, llm LLMProvider) (tags []string, mentions []string, err error) {
	if llm == nil {
		return nil, nil, nil
	}

	summary := buildSummary(obj)
	vocab := ""
	if len(cfg.VocabularyHint) > 0 {
		vocab = fmt.Sprintf(" Prefer tags from this vocabulary where relevant: %s.", strings.Join(cfg.VocabularyHint, ", "))
	}

	prompt := fmt.Sprintf(
		"Analyse this knowledge object and suggest tags and entity @mentions.\n"+
			"Return a JSON object with exactly two fields: \"tags\" (array of strings, max %d) and \"mentions\" (array of strings in @namespace.slug format, max %d).%s\n"+
			"Do not include any explanation or markdown. Output raw JSON only.\n\n"+
			"Content summary:\n%s",
		cfg.MaxTags, cfg.MaxMentions, vocab, summary,
	)

	raw, err := llm.Generate(ctx, prompt)
	if err != nil {
		// LLM errors are soft: log-worthy but not pipeline-fatal.
		return nil, nil, nil
	}

	// Strip optional markdown fences.
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)

	var resp suggestResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		// Unparseable response: soft failure.
		return nil, nil, nil
	}

	// Enforce caps.
	if len(resp.Tags) > cfg.MaxTags {
		resp.Tags = resp.Tags[:cfg.MaxTags]
	}
	if len(resp.Mentions) > cfg.MaxMentions {
		resp.Mentions = resp.Mentions[:cfg.MaxMentions]
	}

	return resp.Tags, resp.Mentions, nil
}

// buildSummary constructs a compact representation of the object for the LLM prompt.
func buildSummary(obj *storage.KnowledgeObject) string {
	var parts []string
	if len(obj.Summaries) > 0 {
		parts = append(parts, obj.Summaries[0])
	}
	content := obj.RawContent
	if len(content) > 800 {
		content = content[:800] + "…"
	}
	if content != "" {
		parts = append(parts, content)
	}
	return strings.Join(parts, "\n\n")
}

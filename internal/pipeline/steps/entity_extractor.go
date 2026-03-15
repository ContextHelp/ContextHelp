package steps

import (
	"context"
	"regexp"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"hop.top/uri"
)

// mentionRe matches @namespace.slug references in text.
// The leading (?:^|[\s(]) ensures we don't match email addresses (user@host).
var mentionRe = regexp.MustCompile(`(?:^|[\s(,;])@([a-z][a-z0-9_-]*\.[a-z][a-z0-9_-]*)`)

// EntityExtractor extracts @namespace.slug mentions from content.
// It first scans for literal @mentions in the text (heuristic), then
// optionally uses an LLM to surface implicit entity references.
type EntityExtractor struct {
	pipeline.BaseContract
	llm providers.LLMProvider
}

func NewEntityExtractor() *EntityExtractor {
	return &EntityExtractor{
		BaseContract: pipeline.NewBaseContract(pipeline.StepContract{
			Requires: []string{"RawContent"},
			Produces: []string{"MentionURIs"},
		}),
	}
}

// NewEntityExtractorWithLLM creates an EntityExtractor that uses an LLM to
// surface implicit entity references beyond literal @mention syntax.
func NewEntityExtractorWithLLM(llm providers.LLMProvider) *EntityExtractor {
	e := NewEntityExtractor()
	e.llm = llm
	return e
}

func (e *EntityExtractor) Name() string { return "entity_extractor" }

func (e *EntityExtractor) Run(ctx context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	seen := make(map[string]bool)
	var mentions []string

	// 1. Heuristic: scan for literal @namespace.slug tokens.
	for _, m := range mentionRe.FindAllStringSubmatch(draft.RawContent, -1) {
		slug := m[1] // capture group 1: namespace.slug without leading @
		if !seen[slug] {
			seen[slug] = true
			mentions = append(mentions, slug)
		}
	}

	// 2. LLM augmentation: ask the LLM to identify implicit entity references
	//    and return them in @namespace.slug format.
	if e.llm != nil && len(draft.RawContent) >= 50 {
		content := draft.RawContent
		if len(content) > 2000 {
			content = content[:2000]
		}
		prompt := "Identify named entities (people, projects, products, organizations, concepts) in " +
			"this content. Return each as @namespace.slug format where namespace is one of: " +
			"person, project, product, org, concept. Return only a comma-separated list, no explanation.\n\n" +
			"Content:\n" + content
		if resp, err := e.llm.Generate(ctx, prompt); err == nil {
			for _, raw := range strings.Split(resp, ",") {
				token := strings.TrimSpace(raw)
				token = strings.TrimPrefix(token, "@")
				if mentionRe.MatchString(" @"+token) && !seen[token] {
					seen[token] = true
					mentions = append(mentions, token)
				}
			}
		}
		// On LLM failure, fall back to heuristic mentions already collected.
	}

	uris := make([]uri.URI, len(mentions))
	for i, m := range mentions {
		uris[i] = slugToMentionURI(m)
	}
	draft.MentionURIs = uris
	return draft, nil
}

// slugToMentionURI converts an extracted slug to a ctxt:// URI.
func slugToMentionURI(slug string) uri.URI {
	slug = strings.TrimPrefix(slug, "@")
	parts := strings.SplitN(slug, ".", 2)
	if len(parts) == 2 {
		return uri.URI{Scheme: "ctxt", Space: "entity", ID: parts[0] + "/" + parts[1]}
	}
	return uri.URI{Scheme: "ctxt", Space: "entity", ID: slug}
}

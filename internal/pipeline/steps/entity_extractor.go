package steps

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/mentions"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
)

// mentionRe matches @namespace.slug references in text.
// The leading (?:^|[\s(]) ensures we don't match email addresses (user@host).
var mentionRe = regexp.MustCompile(`(?:^|[\s(,;])@([a-z][a-z0-9_-]*(?:\.[a-z][a-z0-9_-]*)+)`)

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
			Produces: []string{"Mentions", "Graph"},
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
	var slugs []string

	// 0. T-0190: caller-asserted mentions (e.g. `ctxt analyze --mentions
	//    "@client.acme"`) are pre-populated on draft.Mentions by the worker
	//    before the pipeline runs. Seed `seen` with their slugs so steps 1+2
	//    don't double-insert when the same slug is also detected from text.
	//    Caller-supplied wins on conflict.
	for _, u := range draft.Mentions {
		s := u.String()
		const prefix = "ctxt://entity/"
		if !strings.HasPrefix(s, prefix) {
			continue
		}
		// Convert path form (namespace/slug) back to dot form (namespace.slug)
		// so it dedupes with regex matches that produce dotted slugs.
		slug := strings.TrimPrefix(s, prefix)
		dotted := strings.ReplaceAll(slug, "/", ".")
		seen[dotted] = true
	}

	// 1. Heuristic: scan for literal @namespace.slug tokens.
	for _, m := range mentionRe.FindAllStringSubmatch(draft.RawContent, -1) {
		slug := m[1] // capture group 1: namespace.slug without leading @
		if !seen[slug] {
			seen[slug] = true
			slugs = append(slugs, slug)
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
					slugs = append(slugs, token)
				}
			}
		}
		// On LLM failure, fall back to heuristic slugs already collected.
	}

	// Merge: keep caller-asserted mentions (already on draft.Mentions) and
	// append the text-extracted ones. Order: user-asserted first.
	draft.Mentions = append(draft.Mentions, mentions.ParseSlice(slugs)...)

	// Intra-object graph: one NodeTypeEntityMention node per mention with an
	// EdgeTypeReferences edge pointing to the entity URI.
	// Inter-object edges (object → entity in the edges table) are written by
	// entity_resolver, NOT here — that is the layer boundary (ADR-063).
	if draft.ID != "" && len(draft.Mentions) > 0 {
		if draft.Graph == nil {
			draft.Graph = &pluginapi.ObjectGraph{}
		}
		for i, u := range draft.Mentions {
			nodeID := pluginapi.NewNodeID(draft.ID, pluginapi.NodeTypeEntityMention, i)
			if draft.Graph.FindNode(nodeID) != nil {
				continue
			}
			draft.Graph.Nodes = append(draft.Graph.Nodes, pluginapi.GraphNode{
				ID:       nodeID,
				NodeType: pluginapi.NodeTypeEntityMention,
				Label:    u.String(),
				Order:    i,
			})
			draft.Graph.Edges = append(draft.Graph.Edges, pluginapi.GraphEdge{
				ID:       fmt.Sprintf("%s->%s", nodeID, u.String()),
				FromID:   nodeID,
				ToID:     u.String(),
				EdgeType: pluginapi.EdgeTypeReferences,
			})
		}
	}

	return draft, nil
}

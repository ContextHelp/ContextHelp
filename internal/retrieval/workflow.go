package retrieval

import (
	"context"
	"fmt"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/projection"
	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
)

// EmbeddingProvider wraps providers.EmbeddingProvider to match retrieval usage.
type EmbeddingProvider interface {
	Embed(ctx context.Context, text string) ([]float32, error)
}

// Workflow orchestrates progressive retrieval with optional sufficiency checking.
type Workflow struct {
	config    Config
	store     storage.StorageDriver
	llm       providers.LLMProvider
	embedding EmbeddingProvider
}

// NewWorkflow creates a new retrieval workflow.
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

// Retrieve executes progressive retrieval with sufficiency checking.
// It walks through category → item → resource tiers, stopping early when
// retrieved content is deemed sufficient to answer the query.
// nodeFilter is optional; pass nil to skip node-type filtering.
func (w *Workflow) Retrieve(
	ctx context.Context,
	query string,
	conversationHistory []string,
	filter storage.ObjectFilter,
	nodeFilter ...*pluginapi.NodeAwareFilter,
) (*Result, error) {
	var nf *pluginapi.NodeAwareFilter
	if len(nodeFilter) > 0 {
		nf = nodeFilter[0]
	}
	state := &State{
		OriginalQuery:  query,
		RewrittenQuery: query,
		ActiveQuery:    query,
		NeedsRetrieval: true,
	}

	// Step 1: Route intention check (is retrieval even needed?).
	if w.config.EnableSufficiencyCheck && w.llm != nil {
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

	// When a tier is disabled we skip it but still allow lower tiers to run.
	if !w.config.Categories.Enabled {
		state.ProceedToItems = true
	}
	if !w.config.Items.Enabled {
		state.ProceedToResources = true
	}

	// Step 2: Tier 1 — Categories.
	if w.config.Categories.Enabled {
		if err := w.retrieveCategories(ctx, state, filter, nf); err != nil {
			return nil, fmt.Errorf("retrieve categories: %w", err)
		}

		if w.config.EnableSufficiencyCheck && w.llm != nil && len(state.CategoryHits) > 0 {
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

	// Step 3: Tier 2 — Items.
	if w.config.Items.Enabled && state.ProceedToItems {
		if err := w.retrieveItems(ctx, state, filter, nf); err != nil {
			return nil, fmt.Errorf("retrieve items: %w", err)
		}

		if w.config.EnableSufficiencyCheck && w.llm != nil && len(state.ItemHits) > 0 {
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

	// Step 4: Tier 3 — Resources.
	if w.config.Resources.Enabled && state.ProceedToResources {
		if err := w.retrieveResources(ctx, state, filter, nf); err != nil {
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
			if hit.Data == nil {
				continue
			}
			doc := projection.ProjectDocument(hit.Data)
			snippet := doc.Body
			if snippet == "" && len(doc.Sections) > 0 {
				snippet = doc.Sections[0].Content
			}
			if snippet != "" {
				if len(snippet) > 200 {
					snippet = snippet[:200]
				}
				parts = append(parts, fmt.Sprintf("- %s (score: %.3f)", snippet, hit.Score))
			}
		}
	}

	if len(state.ItemHits) > 0 {
		parts = append(parts, "\n## Items")
		for _, hit := range state.ItemHits {
			if hit.Data == nil {
				continue
			}
			idx := projection.ProjectIndex(hit.Data)
			content := idx.FTSBody
			if len(content) > 200 {
				content = content[:200]
			}
			parts = append(parts, fmt.Sprintf("- %s (score: %.3f)", content, hit.Score))
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

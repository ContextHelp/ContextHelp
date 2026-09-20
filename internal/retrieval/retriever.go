package retrieval

import (
	"context"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
)

// retrieveCategories fetches category-typed objects via the configured method.
func (w *Workflow) retrieveCategories(
	ctx context.Context,
	state *State,
	filter storage.ObjectFilter,
	nf *pluginapi.NodeAwareFilter,
) error {
	return w.ragRetrieve(ctx, state, &state.CategoryHits, "category",
		w.config.Categories.EffectiveTopK(state.QueryMode), filter, nf)
}

// retrieveItems fetches item-typed objects.
func (w *Workflow) retrieveItems(
	ctx context.Context,
	state *State,
	filter storage.ObjectFilter,
	nf *pluginapi.NodeAwareFilter,
) error {
	return w.ragRetrieve(ctx, state, &state.ItemHits, "item",
		w.config.Items.EffectiveTopK(state.QueryMode), filter, nf)
}

// retrieveResources fetches document-typed objects.
func (w *Workflow) retrieveResources(
	ctx context.Context,
	state *State,
	filter storage.ObjectFilter,
	nf *pluginapi.NodeAwareFilter,
) error {
	return w.ragRetrieve(ctx, state, &state.ResourceHits, "document",
		w.config.Resources.EffectiveTopK(state.QueryMode), filter, nf)
}

// ragRetrieve performs a vector similarity search for a given type and accumulates
// hits into the provided slice. If nf is non-nil and has NodeTypes set, the
// VectorSearchNodeAware / FTSSearchNodeAware paths on the store are used so that
// only objects containing matching node types are returned.
func (w *Workflow) ragRetrieve(
	ctx context.Context,
	state *State,
	hits *[]Hit,
	objType string,
	topK int,
	base storage.ObjectFilter,
	nf *pluginapi.NodeAwareFilter,
) error {
	// Embed if not already done, or if the query changed.
	if len(state.QueryVector) == 0 && w.embedding != nil {
		vec, err := w.embedding.Embed(ctx, state.ActiveQuery)
		if err != nil {
			return err
		}
		state.QueryVector = vec
	}

	f := base
	f.Type = objType
	f.Limit = topK

	// When a NodeAwareFilter is active, use the node-aware search path.
	if nf != nil && (len(nf.NodeTypes) > 0 || len(nf.EdgeTypes) > 0) {
		if len(state.QueryVector) > 0 {
			results, err := w.store.Objects().VectorSearchNodeAware(ctx, state.QueryVector, f, *nf)
			if err != nil {
				return err
			}
			for _, r := range results {
				if r.Object == nil {
					continue
				}
				score := 0.0
				if r.Object.Metadata != nil {
					if s, ok := r.Object.Metadata["score"].(float64); ok {
						score = s
					}
				}
				*hits = append(*hits, Hit{ID: r.Object.ID, Score: score, Data: r.Object})
			}
			return nil
		}
		// No vector — node-aware FTS with empty query falls back to list
		// filtered by type. The raw query goes straight to the driver, which
		// applies its own dialect's FTS quoting at the boundary.
		results, err := w.store.Objects().FTSSearchNodeAware(ctx, state.ActiveQuery, f, *nf)
		if err != nil {
			return err
		}
		for _, r := range results {
			if r.Object == nil {
				continue
			}
			*hits = append(*hits, Hit{ID: r.Object.ID, Score: 0, Data: r.Object})
		}
		return nil
	}

	if len(state.QueryVector) == 0 {
		// No embedding available — fall back to a plain List call.
		objs, _, err := w.store.Objects().List(ctx, f)
		if err != nil {
			return err
		}
		for _, obj := range objs {
			*hits = append(*hits, Hit{ID: obj.ID, Score: 0, Data: obj})
		}
		return nil
	}

	objs, err := w.store.Objects().VectorSearch(ctx, state.QueryVector, f)
	if err != nil {
		return err
	}

	for _, obj := range objs {
		score := 0.0
		if obj.Metadata != nil {
			if s, ok := obj.Metadata["score"].(float64); ok {
				score = s
			}
		}
		*hits = append(*hits, Hit{ID: obj.ID, Score: score, Data: obj})
	}
	return nil
}

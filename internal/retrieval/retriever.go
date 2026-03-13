package retrieval

import (
	"context"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// retrieveCategories fetches category-typed objects via the configured method.
func (w *Workflow) retrieveCategories(
	ctx context.Context,
	state *State,
	filter storage.ObjectFilter,
) error {
	if w.config.Method == MethodRAG {
		return w.ragRetrieve(ctx, state, &state.CategoryHits, "category", w.config.Categories.TopK, filter)
	}
	return w.ragRetrieve(ctx, state, &state.CategoryHits, "category", w.config.Categories.TopK, filter)
}

// retrieveItems fetches item-typed objects.
func (w *Workflow) retrieveItems(
	ctx context.Context,
	state *State,
	filter storage.ObjectFilter,
) error {
	if w.config.Method == MethodRAG {
		return w.ragRetrieve(ctx, state, &state.ItemHits, "item", w.config.Items.TopK, filter)
	}
	return w.ragRetrieve(ctx, state, &state.ItemHits, "item", w.config.Items.TopK, filter)
}

// retrieveResources fetches document-typed objects.
func (w *Workflow) retrieveResources(
	ctx context.Context,
	state *State,
	filter storage.ObjectFilter,
) error {
	if w.config.Method == MethodRAG {
		return w.ragRetrieve(ctx, state, &state.ResourceHits, "document", w.config.Resources.TopK, filter)
	}
	return w.ragRetrieve(ctx, state, &state.ResourceHits, "document", w.config.Resources.TopK, filter)
}

// ragRetrieve performs a vector similarity search for a given type and accumulates
// hits into the provided slice. If the query vector is not yet populated, it
// embeds the active query first.
func (w *Workflow) ragRetrieve(
	ctx context.Context,
	state *State,
	hits *[]Hit,
	objType string,
	topK int,
	base storage.ObjectFilter,
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

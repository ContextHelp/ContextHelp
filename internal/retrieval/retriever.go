package retrieval

import (
	"context"
	"fmt"
	"log/slog"

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
//
// The query is embedded once per retrieval, under the default model read at
// that moment. When the semantic leg is unavailable (no default model,
// provider failure, missing index or no coverage) the reason is recorded in
// state.Semantic, logged, and the tier falls back to FTS (node-aware) or a
// plain list.
func (w *Workflow) ragRetrieve(
	ctx context.Context,
	state *State,
	hits *[]Hit,
	objType string,
	topK int,
	base storage.ObjectFilter,
	nf *pluginapi.NodeAwareFilter,
) error {
	if state.Semantic == nil {
		if err := w.embedQuery(ctx, state); err != nil {
			return err
		}
	}

	f := base
	f.Type = objType
	f.Limit = topK
	nodeAware := nf != nil && (len(nf.NodeTypes) > 0 || len(nf.EdgeTypes) > 0)

	if len(state.QueryVector.Vector) > 0 {
		done, err := w.vectorRetrieve(ctx, state, hits, f, nf, nodeAware)
		if err != nil || done {
			return err
		}
	}

	if nodeAware {
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

// embedQuery embeds the active query under the current default model and
// records the semantic leg's status on state.
func (w *Workflow) embedQuery(ctx context.Context, state *State) error {
	qv, rep, err := w.semantic.Embed(ctx, state.ActiveQuery)
	if err != nil {
		return err
	}
	state.QueryVector = qv
	w.recordSemantic(state, rep)
	return nil
}

// recordSemantic stores rep on state and logs a skipped leg, so retrieval
// never degrades to FTS or a plain list silently.
func (w *Workflow) recordSemantic(state *State, rep SemanticReport) {
	state.Semantic = &rep
	if !rep.OK() {
		state.QueryVector = QueryVector{}
		slog.Warn("retrieval: "+rep.Notice, "status", string(rep.Status), "model_id", rep.ModelID)
	}
}

// vectorRetrieve runs one tier through the default model's index. It
// reports done=false, after recording why, when the index is missing, the
// dimension does not fit, or the model has no vectors at all, so the caller
// falls back.
func (w *Workflow) vectorRetrieve(
	ctx context.Context,
	state *State,
	hits *[]Hit,
	f storage.ObjectFilter,
	nf *pluginapi.NodeAwareFilter,
	nodeAware bool,
) (bool, error) {
	q := storage.VectorQuery{
		ModelID: state.QueryVector.ModelID,
		Vector:  state.QueryVector.Vector,
		TopK:    f.Limit,
	}

	var objs []*storage.KnowledgeObject
	var err error
	if nodeAware {
		var results []*pluginapi.NodeAwareResult
		results, err = w.store.Objects().VectorSearchNodeAware(ctx, q, f, *nf)
		for _, r := range results {
			if r.Object != nil {
				objs = append(objs, r.Object)
			}
		}
	} else {
		objs, err = w.store.Objects().VectorSearch(ctx, q, f)
	}
	if rep, handled := classifyVectorError(q.ModelID, err); handled {
		w.recordSemantic(state, rep)
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if len(objs) == 0 {
		covered, err := hasVectors(ctx, w.store.Embeddings(), q)
		if err != nil {
			return false, err
		}
		if !covered {
			w.recordSemantic(state, semanticSkipped(SemanticLowCoverage, q.ModelID,
				fmt.Sprintf("model %s has no stored vectors; run 'ctxt embeddings migrate'", q.ModelID)))
			return false, nil
		}
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
	return true, nil
}

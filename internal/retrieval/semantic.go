package retrieval

import (
	"context"
	"errors"
	"fmt"

	"github.com/ideacrafterslabs/ctxt/internal/embeddings"
	"github.com/ideacrafterslabs/ctxt/internal/embeddings/registry"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// SemanticStatus says whether a query's semantic (vector) leg ran against
// the default model's index, and if not, why.
type SemanticStatus string

// Semantic leg statuses. Anything but SemanticOK means the leg contributed
// no hits and the caller shows a notice (ADR-071, amendment 2026-09-26: the
// query path never degrades to full-text silently).
const (
	SemanticOK                SemanticStatus = "ok"
	SemanticNoDefaultModel    SemanticStatus = "no_default_model"
	SemanticProviderError     SemanticStatus = "provider_error"
	SemanticDimensionMismatch SemanticStatus = "dimension_mismatch"
	SemanticIndexMissing      SemanticStatus = "index_missing"
	SemanticLowCoverage       SemanticStatus = "low_coverage"
)

// SemanticReport describes the semantic leg of one query.
type SemanticReport struct {
	Status SemanticStatus `json:"status"`
	// ModelID is the default model the leg read, when there was one.
	ModelID string `json:"model_id,omitempty"`
	// Detail explains a non-ok status.
	Detail string `json:"detail,omitempty"`
	// Notice is the one-line operator notice for a non-ok status; empty
	// when the leg ran.
	Notice string `json:"notice,omitempty"`
}

// OK reports whether the semantic leg ran.
func (r SemanticReport) OK() bool { return r.Status == SemanticOK }

func semanticOK(modelID string) SemanticReport {
	return SemanticReport{Status: SemanticOK, ModelID: modelID}
}

func semanticSkipped(status SemanticStatus, modelID, detail string) SemanticReport {
	return SemanticReport{
		Status:  status,
		ModelID: modelID,
		Detail:  detail,
		Notice:  fmt.Sprintf("semantic search unavailable (%s): %s; results are full-text only", status, detail),
	}
}

// QueryVector is an embedded query bound to the model whose index it
// searches.
type QueryVector struct {
	ModelID string
	Vector  []float32
}

// SemanticSource is how the query path reaches the default model's index.
// It holds no model state: every query reads the current default, so a
// set-default flip in another process applies to the next query.
type SemanticSource struct {
	// Models answers which model is the default. Nil means no model is
	// registered.
	Models embeddings.ModelSource
	// Resolver builds the default model's provider from its registry
	// entry; runtime overrides may change transport only.
	Resolver embeddings.ProviderResolver
	// Blend, when set, rewrites the query vector before search (session
	// context). It receives the model the vector belongs to so it never
	// mixes vectors from different models.
	Blend func(modelID string, vec []float32) []float32
}

// Embed reads the default model, resolves its provider, embeds text and
// checks the vector against the registry dimension. A non-ok report means
// there is no usable query vector; the error is for failures to read the
// registry itself.
func (s SemanticSource) Embed(ctx context.Context, text string) (QueryVector, SemanticReport, error) {
	if s.Models == nil {
		return QueryVector{}, semanticSkipped(SemanticNoDefaultModel, "", noDefaultDetail), nil
	}
	m, err := s.Models.Default(ctx)
	if errors.Is(err, registry.ErrNoDefaultModel) {
		return QueryVector{}, semanticSkipped(SemanticNoDefaultModel, "", noDefaultDetail), nil
	}
	if err != nil {
		return QueryVector{}, SemanticReport{}, fmt.Errorf("read default embedding model: %w", err)
	}
	if m.Dimension <= 0 {
		return QueryVector{}, semanticSkipped(SemanticIndexMissing, m.ModelID, noDimensionDetail(m.ModelID)), nil
	}
	if s.Resolver == nil {
		return QueryVector{}, semanticSkipped(SemanticProviderError, m.ModelID,
			fmt.Sprintf("model %s: no embedding provider resolver", m.ModelID)), nil
	}
	p, err := s.Resolver.ForModel(ctx, *m)
	if err != nil {
		return QueryVector{}, semanticSkipped(SemanticProviderError, m.ModelID,
			fmt.Sprintf("model %s: %v", m.ModelID, err)), nil
	}
	vec, err := p.Embed(ctx, text)
	if err != nil {
		return QueryVector{}, semanticSkipped(SemanticProviderError, m.ModelID,
			fmt.Sprintf("model %s: embed query: %v", m.ModelID, err)), nil
	}
	if len(vec) == 0 {
		return QueryVector{}, semanticSkipped(SemanticProviderError, m.ModelID,
			fmt.Sprintf("model %s: provider returned no vector", m.ModelID)), nil
	}
	if len(vec) != m.Dimension {
		return QueryVector{}, semanticSkipped(SemanticDimensionMismatch, m.ModelID,
			fmt.Sprintf("model %s: query vector has %d dimensions, registry says %d", m.ModelID, len(vec), m.Dimension)), nil
	}
	return QueryVector{ModelID: m.ModelID, Vector: vec}, semanticOK(m.ModelID), nil
}

const noDefaultDetail = "no default embedding model; register one with 'ctxt embeddings register' and make it the default"

// noDimensionDetail explains an index_missing model without a dimension.
// The model's registry row cannot be fixed in place; registering the model
// again under a new model_id measures its dimension.
func noDimensionDetail(modelID string) string {
	return fmt.Sprintf("model %s has no measured dimension, so it has no index; "+
		"register the model under a new model_id, which measures it, then migrate to it and set it as the default", modelID)
}

// noIndexDetail explains an index_missing model with a dimension. Opening
// the database rebuilds every registered model's missing index, and the
// query path opened it, so the open skipped this model and logged why.
func noIndexDetail(modelID string) string {
	return fmt.Sprintf("model %s has no vector index and opening the database did not rebuild it; "+
		"the 'embedding index skipped' warning says why", modelID)
}

// Search runs the semantic leg for text: Embed, the optional Blend, then
// VectorSearch over the default model's index with filter. A model whose
// index is missing or holds no vectors yields a non-ok report and no hits;
// other storage failures are errors.
func (s SemanticSource) Search(ctx context.Context, drv storage.StorageDriver, text string, filter storage.ObjectFilter) ([]*storage.KnowledgeObject, SemanticReport, error) {
	qv, rep, err := s.Embed(ctx, text)
	if err != nil || !rep.OK() {
		return nil, rep, err
	}
	vec := qv.Vector
	if s.Blend != nil {
		vec = s.Blend(qv.ModelID, qv.Vector)
	}
	q := storage.VectorQuery{ModelID: qv.ModelID, Vector: vec, TopK: filter.Limit}
	objs, err := drv.Objects().VectorSearch(ctx, q, filter)
	if rep, handled := classifyVectorError(qv.ModelID, err); handled {
		return nil, rep, nil
	}
	if err != nil {
		return nil, rep, fmt.Errorf("vector search: %w", err)
	}
	if len(objs) == 0 {
		covered, err := hasVectors(ctx, drv.Embeddings(), q)
		if err != nil {
			return nil, rep, err
		}
		if !covered {
			return nil, semanticSkipped(SemanticLowCoverage, qv.ModelID,
				fmt.Sprintf("model %s has no stored vectors; run 'ctxt embeddings migrate'", qv.ModelID)), nil
		}
	}
	return objs, rep, nil
}

// classifyVectorError maps the EmbeddingStore sentinels a search can hit to
// a skipped-leg report.
func classifyVectorError(modelID string, err error) (SemanticReport, bool) {
	switch {
	case errors.Is(err, storage.ErrEmbeddingIndexMissing):
		return semanticSkipped(SemanticIndexMissing, modelID, noIndexDetail(modelID)), true
	case errors.Is(err, storage.ErrEmbeddingDimension):
		return semanticSkipped(SemanticDimensionMismatch, modelID,
			fmt.Sprintf("model %s: %v", modelID, err)), true
	}
	return SemanticReport{}, false
}

// hasVectors reports whether the model's index holds at least one vector.
// It runs only when a search came back empty, to tell "nothing matched the
// filter" from "the default model has no coverage". A nil store (test
// doubles without embeddings) counts as covered.
func hasVectors(ctx context.Context, store storage.EmbeddingStore, q storage.VectorQuery) (bool, error) {
	if store == nil {
		return true, nil
	}
	hits, err := store.Search(ctx, storage.VectorQuery{ModelID: q.ModelID, Vector: q.Vector, TopK: 1})
	if err != nil {
		return false, fmt.Errorf("vector coverage probe: %w", err)
	}
	return len(hits) > 0, nil
}

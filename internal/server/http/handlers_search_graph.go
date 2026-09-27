package http

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"

	"github.com/ideacrafterslabs/ctxt/internal/registry"
	"github.com/ideacrafterslabs/ctxt/internal/retrieval"
	"github.com/ideacrafterslabs/ctxt/internal/searchgraph"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Search graph limits. Defaults are `ctxt find --graph`'s. The maxima
// bound what one request can make the server build and send: a document
// at the maxima is a few MB, against ~0.5 MB at the defaults. Objects are
// further bounded by the search's candidate pools (search.candidate_pool,
// 50 + 50 by default), whatever max_nodes allows.
const (
	searchGraphDefaultLimit = service.DefaultFindLimit
	searchGraphMaxNodes     = 1000
	searchGraphMaxEdges     = 10000
)

// searchGraphRejectedParams are search parameters the graph does not
// take: it is always the hybrid trace, with every candidate in one page.
var searchGraphRejectedParams = []string{"mode", "offset"}

// EntityVisibilityFunc returns the entity policy for one request. It must
// never return nil: the handler builds with RequireEntityVisibility, so a
// nil policy fails the request instead of serving every entity.
type EntityVisibilityFunc func(*http.Request) searchgraph.EntityVisibility

// GateEntityVisibility is the search graph's entity policy. With an
// inbound gate wired, an entity is visible when the authenticated
// principal is entitled to its namespace — the unmetered check
// ListEntities filters with — and an entity without a stored record is
// hidden (fail closed: no namespace to check). Without a gate (private
// instance), or for a principal that bypasses it (admin), every entity
// is visible, as on every other entity route.
func GateEntityVisibility(gate *registry.InboundGate) EntityVisibilityFunc {
	return func(r *http.Request) searchgraph.EntityVisibility {
		gate := entityGate(r, gate)
		if gate == nil {
			return func(context.Context, string, *storage.Entity) bool { return true }
		}
		principal := gatePrincipal(r)
		return func(ctx context.Context, _ string, ent *storage.Entity) bool {
			return ent != nil && gate.Check(ctx, principal, ent.Namespace) == nil
		}
	}
}

// SearchGraph handles GET /api/v1/search/graph: the hybrid search trace
// as a JGF v2.1 document (vocabulary ctxt.search-graph/v1), built by the
// same pipeline as `ctxt find --graph`: service.Find with Trace, sem as
// its semantic leg (as POST /find), then searchgraph.Build. The body is
// the bare document, not the {data, total} envelope; errors use the
// standard envelope. Search is not metered, and neither is the graph:
// entity visibility is the unmetered namespace check.
func SearchGraph(svc *service.Service, sem retrieval.SemanticSource, visibility EntityVisibilityFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		params := r.URL.Query()
		q := params.Get("q")
		if q == "" {
			WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "q parameter is required")
			return
		}
		req, opts, err := parseSearchGraphRequest(params, q)
		if err != nil {
			WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
			return
		}
		opts.RequireEntityVisibility = true
		opts.EntityVisible = visibility(r)

		// fallback_to_fts keeps its built-in default (on): a semantic leg
		// that cannot run gives a full-text-only graph, never an error.
		res, err := svc.Find(r.Context(), req, sem)
		switch {
		case errors.Is(err, service.ErrInvalidFind):
			WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
			return
		case err != nil:
			WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}

		doc, err := searchgraph.Build(r.Context(), res.Trace, searchgraph.SourceFrom(svc.Store), opts)
		switch {
		case errors.Is(err, searchgraph.ErrSimilarUnsupported):
			WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "similar edges need stored embeddings, and this store cannot read them")
			return
		case err != nil:
			WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}
		var buf bytes.Buffer
		if err := doc.Encode(&buf, false); err != nil {
			WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(buf.Bytes())
	}
}

// parseSearchGraphRequest reads the find request the graph runs — a
// hybrid, traced find; service.Find validates its values — then the
// graph's own caps. Out-of-range caps are errors, never clamped.
func parseSearchGraphRequest(params url.Values, q string) (service.FindRequest, searchgraph.Options, error) {
	var opts searchgraph.Options
	for _, p := range searchGraphRejectedParams {
		if params.Has(p) {
			return service.FindRequest{}, opts, fmt.Errorf("%s does not apply to the search graph: it is always the full hybrid trace", p)
		}
	}
	req := service.FindRequest{
		Query:   q,
		Mode:    service.FindModeHybrid,
		Profile: params.Get("profile"),
		Filter: service.FindFilter{
			MetaType:   params.Get("meta_type"),
			Topic:      params.Get("topic"),
			Person:     params.Get("person"),
			SourceType: params.Get("source_type"),
			Since:      params.Get("since"),
			Until:      params.Get("until"),
		},
		Trace: true,
	}
	var err error
	if req.Limit, err = graphIntParam(params, "limit", searchGraphDefaultLimit); err != nil {
		return req, opts, err
	}
	if params.Has("min_score") {
		v, err := strconv.ParseFloat(params.Get("min_score"), 64)
		if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
			return req, opts, fmt.Errorf("min_score: %q is not a number", params.Get("min_score"))
		}
		req.Search.MinScore = &v
	}
	if opts.MaxNodes, err = boundedIntParam(params, "max_nodes", searchgraph.DefaultMaxNodes, searchGraphMaxNodes); err != nil {
		return req, opts, err
	}
	if opts.MaxEdges, err = boundedIntParam(params, "max_edges", searchgraph.DefaultMaxEdges, searchGraphMaxEdges); err != nil {
		return req, opts, err
	}
	if params.Has("similar") {
		if opts.Similar, err = strconv.ParseBool(params.Get("similar")); err != nil {
			return req, opts, fmt.Errorf("similar: %q is not a boolean", params.Get("similar"))
		}
	}
	opts.SimilarThreshold = searchgraph.DefaultSimilarThreshold
	if params.Has("similar_threshold") {
		if !opts.Similar {
			return req, opts, errors.New("similar_threshold has no effect without similar=true")
		}
		v, err := strconv.ParseFloat(params.Get("similar_threshold"), 64)
		if err != nil || !(v > 0 && v <= 1) {
			return req, opts, fmt.Errorf("similar_threshold: %q must be a number in (0,1]", params.Get("similar_threshold"))
		}
		opts.SimilarThreshold = v
	}
	return req, opts, nil
}

// graphIntParam reads an integer, def when absent.
func graphIntParam(params url.Values, name string, def int) (int, error) {
	if !params.Has(name) {
		return def, nil
	}
	v, err := strconv.Atoi(params.Get(name))
	if err != nil {
		return 0, fmt.Errorf("%s: %q is not an integer", name, params.Get(name))
	}
	return v, nil
}

// boundedIntParam reads an integer in [1, maxV], def when absent.
func boundedIntParam(params url.Values, name string, def, maxV int) (int, error) {
	v, err := graphIntParam(params, name, def)
	if err != nil {
		return 0, err
	}
	if v < 1 || v > maxV {
		return 0, fmt.Errorf("%s must be between 1 and %d, got %d", name, maxV, v)
	}
	return v, nil
}

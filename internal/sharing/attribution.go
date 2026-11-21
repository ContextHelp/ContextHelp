package sharing

import (
	"context"
	"slices"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/search"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Per-object attribution (ADR-080 §5). A viewer under several grants
// sees each object through the grants that cover it, never through the
// union of all it holds: a grant showing every field of public notes
// shows nothing more of a private note another grant shows the title
// of. Which grants cover a row is asked of the database, with each
// grant's own compiled predicate, the same SQL the viewer's predicate
// is the union of, so the visibility and the render decisions come
// from one evaluator and cannot drift apart.

// attributionChunk bounds the ids of one attribution query.
const attributionChunk = 500

// ObjectPlan returns the plan an object renders under when covering
// says which of v's grants cover it (covering[i] for Plan().Grants[i];
// a grant past its end covers nothing): Fields and Masked merged across
// the covering grants alone and narrowed by the key policy, the ceiling,
// capabilities and part exclusions as in Plan. When every grant covers
// it, it is Plan itself; when none does, it renders only what every
// object keeps (fail closed).
func (v Viewer) ObjectPlan(covering []bool) RenderPlan {
	if v.view == nil {
		return RenderPlan{}
	}
	w := v.view
	cover := make([]Grant, 0, len(w.grants))
	for i, g := range w.grants {
		if i < len(covering) && covering[i] {
			cover = append(cover, g)
		}
	}
	plan := w.plan
	if len(cover) == len(w.grants) {
		return plan
	}
	fields, _, masked := unionPlan(cover)
	if w.keyPolicy != nil {
		fields = intersectFields(fields, *w.keyPolicy)
	}
	plan.Fields, plan.Masked = fields, masked
	return plan
}

// ForCapability returns v as a surface capability c opens sees it: the
// predicate over the grants that hold c alone (still ANDed with the
// ceiling and the key policy), so the surface answers only the objects
// such a grant covers; the render plan is v's, so an object it answers
// renders through every grant covering it. Unrestricted, the zero
// viewer and a viewer whose every grant holds c are returned as they
// are. Whether v may open the surface at all is Allows(c).
func (v Viewer) ForCapability(c string) Viewer {
	if v.unrestricted || v.view == nil || c == "" {
		return v
	}
	w := v.view
	union := make([]search.Node, 0, len(w.grants))
	for i, g := range w.grants {
		if g.Policy.Capabilities.Has(c) {
			union = append(union, w.grantNodes[i])
		}
	}
	if len(union) == len(w.grants) {
		return v
	}
	n := *w
	n.key = w.key + ";capability=" + c
	if err := n.compilePredicate(union); err != nil {
		// Each node compiled when the viewer did; should a subset not,
		// the surface matches nothing (fail closed).
		_ = n.compilePredicate(nil)
	}
	return Viewer{view: &n}
}

// Has reports whether c opens capability (storage.CapabilityEntities,
// "search:fts", ...).
func (c Capabilities) Has(capability string) bool {
	if mode, ok := strings.CutPrefix(capability, storage.CapabilitySearchPrefix); ok {
		return slices.Contains(c.Search, mode)
	}
	switch capability {
	case storage.CapabilityEntities:
		return c.Entities
	case storage.CapabilityExport:
		return c.Export
	case storage.CapabilityMCP:
		return c.MCP
	case storage.CapabilityEvents:
		return c.Events
	}
	return false
}

// grantPredicates returns each of v's grants' predicate in dialect d,
// in plan order: what MatchBySQL attributes rows with.
func (v Viewer) grantPredicates(d search.Dialect) []storage.SQLPredicate {
	if v.view == nil {
		return nil
	}
	return v.view.grantSQL[d]
}

// attribution says which grants cover each object a read released. A
// nil by means every grant covers every row: the viewer has one grant
// (or none), so the read's own predicate already decided it and no
// query runs.
type attribution struct {
	by  map[string][]bool
	all []bool
}

// of returns the coverage of object id: nil (no grant) for a row the
// attribution query did not return, a row that stopped matching between
// the read and the query.
func (a attribution) of(id string) []bool {
	if a.by == nil {
		return a.all
	}
	return a.by[id]
}

// attribute runs the attribution of ids for d's viewer: one MatchBySQL
// through the scoped object store (the viewer's scope ANDed in, so a
// row that left the view between the two queries covers nothing) over
// every grant's predicate, per attributionChunk ids. A viewer with at
// most one grant pays nothing.
func (d renderDriver) attribute(ctx context.Context, ids []string) (attribution, error) {
	all := make([]bool, len(d.preds))
	for i := range all {
		all[i] = true
	}
	if len(d.preds) <= 1 {
		return attribution{all: all}, nil
	}
	a := attribution{by: make(map[string][]bool, len(ids))}
	objects := d.StorageDriver.Objects()
	for chunk := range slices.Chunk(ids, attributionChunk) {
		got, err := objects.MatchBySQL(ctx, chunk, d.preds)
		if err != nil {
			return attribution{}, err
		}
		for id, m := range got {
			a.by[id] = m
		}
	}
	return a, nil
}

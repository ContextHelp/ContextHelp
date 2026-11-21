package sharing

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/projection"
	"github.com/ideacrafterslabs/ctxt/internal/search"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Scoped returns d as the caller of ctx may see it, and is the only
// storage path a request handler has (Service.Store). An unrestricted
// viewer gets d itself and pays nothing. A restricted viewer gets the
// scoped driver (storage.ScopedDriver: visible rows only) wrapped so
// that every object it releases is rendered first (Render) and
// recorded in the request's Ledger with every entity and edge it
// releases; the rows an edge, a neighbour pair or an alias refers to
// are released through their own scoped reads. A handler therefore cannot obtain
// an unrendered object: there is no accessor that returns one. A
// context no transport attached a viewer to gets a driver whose every
// read refuses. prov supplies the step contracts the render layer
// applies the derived rule with. Each object renders through the grants
// that cover it: after a read, one attribution query per page asks the
// database which of the viewer's grants cover each row it released
// (renderDriver.attribute); a viewer with one grant skips it.
func Scoped(ctx context.Context, d storage.StorageDriver, prov Provenance) storage.StorageDriver {
	v, ok := FromContext(ctx)
	if !ok {
		return storage.ScopedDriver(nil, d)
	}
	if v.IsUnrestricted() {
		return d
	}
	l := LedgerFrom(ctx)
	if l == nil {
		l = NewLedger()
	}
	l.MarkScoped()
	return renderDriver{
		StorageDriver: storage.ScopedDriver(v, d), v: v, ledger: l, prov: prov,
		preds: v.grantPredicates(search.DialectFor(d)),
	}
}

// renderDriver is a restricted viewer's driver: the scoped driver with
// the stores that release rows wrapped to render and record them.
type renderDriver struct {
	storage.StorageDriver
	v      Viewer
	ledger *Ledger
	prov   Provenance
	// preds are the viewer's grants' predicates in the driver's
	// dialect, in plan order: what attribute runs.
	preds []storage.SQLPredicate
}

func (d renderDriver) Objects() storage.ObjectStore {
	return renderObjects{ObjectStore: d.StorageDriver.Objects(), d: d}
}

func (d renderDriver) Entities() storage.EntityStore {
	return ledgerEntities{EntityStore: d.StorageDriver.Entities(), l: d.ledger}
}

func (d renderDriver) Edges() storage.EdgeStore {
	return ledgerEdges{EdgeStore: d.StorageDriver.Edges(), d: d}
}

func (d renderDriver) Proximity() storage.ProximityStore {
	return ledgerProximity{ProximityStore: d.StorageDriver.Proximity(), d: d}
}

func (d renderDriver) Aliases() storage.AliasStore {
	return ledgerAliases{AliasStore: d.StorageDriver.Aliases(), d: d}
}

// releaseObject records object id as released once a scoped read of
// it succeeds. The ledger's objects come only from object reads, which
// run the viewer's predicate: a row an edge, a neighbour pair or an
// alias refers to is checked here, never taken on the referrer's word,
// so a scoped join that went missing in one store still leaves the
// backstop something to catch.
func (d renderDriver) releaseObject(ctx context.Context, id string) {
	if id == "" || d.ledger.HasObject(id) {
		return
	}
	_, _ = d.Objects().Get(ctx, id)
}

// releaseEntity is releaseObject for an entity slug or URI.
func (d renderDriver) releaseEntity(ctx context.Context, slug string) {
	slug = strings.TrimPrefix(slug, EntityURIPrefix)
	if slug == "" || d.ledger.HasEntity(slug) {
		return
	}
	_, _ = d.Entities().Get(ctx, slug)
}

// releaseEndpoint releases an edge end: an entity by URI, else an
// object.
func (d renderDriver) releaseEndpoint(ctx context.Context, typ, id string) {
	if typ == storage.EdgeEndpointEntity {
		d.releaseEntity(ctx, id)
		return
	}
	d.releaseObject(ctx, id)
}

// renderOne renders one object under its coverage and records it.
func (d renderDriver) renderOne(obj *storage.KnowledgeObject, covering []bool) *storage.KnowledgeObject {
	r := Render(d.v, obj, covering, d.prov)
	d.ledger.AddObject(r.Object.ID)
	d.ledger.AddWithheld(r.Withheld)
	return r.Object
}

// render attributes and renders one object.
func (d renderDriver) render(ctx context.Context, obj *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	if obj == nil {
		return nil, nil
	}
	a, err := d.attribute(ctx, []string{obj.ID})
	if err != nil {
		return nil, err
	}
	return d.renderOne(obj, a.of(obj.ID)), nil
}

// renderAll attributes a page of objects in one query and renders each.
func (d renderDriver) renderAll(ctx context.Context, objs []*storage.KnowledgeObject) ([]*storage.KnowledgeObject, error) {
	if objs == nil {
		return nil, nil
	}
	ids := make([]string, 0, len(objs))
	for _, o := range objs {
		if o != nil {
			ids = append(ids, o.ID)
		}
	}
	a, err := d.attribute(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make([]*storage.KnowledgeObject, 0, len(objs))
	for _, o := range objs {
		if o != nil {
			out = append(out, d.renderOne(o, a.of(o.ID)))
		}
	}
	return out, nil
}

// renderNodeAware renders each result's object and keeps only the node
// hits that point at a node the rendered graph still holds; the
// document view is rebuilt from the rendered object.
func (d renderDriver) renderNodeAware(ctx context.Context, res []*storage.NodeAwareResult) ([]*storage.NodeAwareResult, error) {
	if res == nil {
		return nil, nil
	}
	ids := make([]string, 0, len(res))
	for _, r := range res {
		if r != nil && r.Object != nil {
			ids = append(ids, r.Object.ID)
		}
	}
	a, err := d.attribute(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make([]*storage.NodeAwareResult, 0, len(res))
	for _, r := range res {
		if r == nil || r.Object == nil {
			continue
		}
		obj := d.renderOne(r.Object, a.of(r.Object.ID))
		nr := &storage.NodeAwareResult{Object: obj}
		if r.DocumentView != nil {
			dv := projection.ProjectDocument(obj)
			nr.DocumentView = &dv
		}
		for _, h := range r.NodeHits {
			if obj.Graph != nil && obj.Graph.FindNode(nodeIDOfRef(h.NodeRef)) != nil {
				nr.NodeHits = append(nr.NodeHits, h)
			}
		}
		out = append(out, nr)
	}
	return out, nil
}

// nodeIDOfRef turns a node URI (ctxt:node/<object>/<type>/<n>) into
// the node ID the graph holds (<object>/<type>/<n>).
func nodeIDOfRef(ref string) string {
	return strings.TrimPrefix(ref, "ctxt:node/")
}

// renderObjects is the object store of a restricted viewer: every read
// that releases an object renders it first, and a write of a rendered
// object is refused unless the viewer's policy projects every field of
// its type, since writing a projected copy back would erase what the
// view withheld.
type renderObjects struct {
	storage.ObjectStore
	d renderDriver
}

// renderedReads names the ObjectStore methods that release objects;
// each is overridden here. The package's tests check the list against
// the interface, so a method added to ObjectStore that returns objects
// fails them until it is rendered too.
var renderedReads = []string{
	"Get", "GetByContentHash", "GetBySourceKey", "List", "ListBySQL",
	"VectorSearch", "FTSSearch", "FTSSearchNodeAware", "VectorSearchNodeAware",
	"ListDueReminders", "ListPendingReminders",
}

func (s renderObjects) Get(ctx context.Context, id string) (*storage.KnowledgeObject, error) {
	o, err := s.ObjectStore.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return s.d.render(ctx, o)
}

func (s renderObjects) GetByContentHash(ctx context.Context, hash, profileID string) (*storage.KnowledgeObject, error) {
	o, err := s.ObjectStore.GetByContentHash(ctx, hash, profileID)
	if err != nil {
		return nil, err
	}
	return s.d.render(ctx, o)
}

func (s renderObjects) GetBySourceKey(ctx context.Context, key, profileID string) (*storage.KnowledgeObject, error) {
	o, err := s.ObjectStore.GetBySourceKey(ctx, key, profileID)
	if err != nil {
		return nil, err
	}
	return s.d.render(ctx, o)
}

func (s renderObjects) List(ctx context.Context, filter storage.ObjectFilter) ([]*storage.KnowledgeObject, int, error) {
	objs, total, err := s.ObjectStore.List(ctx, filter)
	if err != nil {
		return nil, 0, err
	}
	out, err := s.d.renderAll(ctx, objs)
	if err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

func (s renderObjects) ListBySQL(ctx context.Context, where string, args []any, filter storage.ObjectFilter) ([]*storage.KnowledgeObject, int, error) {
	objs, total, err := s.ObjectStore.ListBySQL(ctx, where, args, filter)
	if err != nil {
		return nil, 0, err
	}
	out, err := s.d.renderAll(ctx, objs)
	if err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

func (s renderObjects) VectorSearch(ctx context.Context, q storage.VectorQuery, filter storage.ObjectFilter) ([]*storage.KnowledgeObject, error) {
	objs, err := s.ObjectStore.VectorSearch(ctx, q, filter)
	if err != nil {
		return nil, err
	}
	return s.d.renderAll(ctx, objs)
}

func (s renderObjects) FTSSearch(ctx context.Context, query string, filter storage.ObjectFilter) ([]*storage.KnowledgeObject, error) {
	objs, err := s.ObjectStore.FTSSearch(ctx, query, filter)
	if err != nil {
		return nil, err
	}
	return s.d.renderAll(ctx, objs)
}

func (s renderObjects) FTSSearchNodeAware(ctx context.Context, query string, filter storage.ObjectFilter, naf storage.NodeAwareFilter) ([]*storage.NodeAwareResult, error) {
	res, err := s.ObjectStore.FTSSearchNodeAware(ctx, query, filter, naf)
	if err != nil {
		return nil, err
	}
	return s.d.renderNodeAware(ctx, res)
}

func (s renderObjects) VectorSearchNodeAware(ctx context.Context, q storage.VectorQuery, filter storage.ObjectFilter, naf storage.NodeAwareFilter) ([]*storage.NodeAwareResult, error) {
	res, err := s.ObjectStore.VectorSearchNodeAware(ctx, q, filter, naf)
	if err != nil {
		return nil, err
	}
	return s.d.renderNodeAware(ctx, res)
}

func (s renderObjects) ListDueReminders(ctx context.Context, now time.Time) ([]*storage.KnowledgeObject, error) {
	objs, err := s.ObjectStore.ListDueReminders(ctx, now)
	if err != nil {
		return nil, err
	}
	return s.d.renderAll(ctx, objs)
}

func (s renderObjects) ListPendingReminders(ctx context.Context) ([]*storage.KnowledgeObject, error) {
	objs, err := s.ObjectStore.ListPendingReminders(ctx)
	if err != nil {
		return nil, err
	}
	return s.d.renderAll(ctx, objs)
}

// Update refuses to write back an object the viewer sees rendered: the
// stored row holds what the view withheld, and the copy a handler
// edited is the projection. Only when the grants covering the object
// project every field of its type is the row read whole, and may it be
// written.
func (s renderObjects) Update(ctx context.Context, obj *storage.KnowledgeObject) error {
	if obj != nil {
		a, err := s.d.attribute(ctx, []string{obj.ID})
		if err != nil {
			return fmt.Errorf("update object %s: %w", obj.ID, err)
		}
		if !newFieldSet(s.d.v.ObjectPlan(a.of(obj.ID)).FieldsFor(obj.Type)).all {
			return fmt.Errorf("update object %s: the viewer sees it rendered, a write back would erase withheld fields: %w", obj.ID, storage.ErrRestrictedViewer)
		}
	}
	return s.ObjectStore.Update(ctx, obj)
}

// ledgerEntities records every entity the scoped entity store releases.
type ledgerEntities struct {
	storage.EntityStore
	l *Ledger
}

func (s ledgerEntities) Get(ctx context.Context, slug string) (*storage.Entity, error) {
	e, err := s.EntityStore.Get(ctx, slug)
	if err == nil && e != nil {
		s.l.AddEntity(e.Slug)
	}
	return e, err
}

func (s ledgerEntities) List(ctx context.Context, filter storage.EntityFilter) ([]*storage.Entity, error) {
	es, err := s.EntityStore.List(ctx, filter)
	for _, e := range es {
		if e != nil {
			s.l.AddEntity(e.Slug)
		}
	}
	return es, err
}

func (s ledgerEntities) Resolve(ctx context.Context, mention string) (*storage.Entity, error) {
	e, err := s.EntityStore.Resolve(ctx, mention)
	if err == nil && e != nil {
		s.l.AddEntity(e.Slug)
	}
	return e, err
}

// ledgerEdges records every edge the scoped edge store releases and
// releases both of its ends through their own scoped reads.
type ledgerEdges struct {
	storage.EdgeStore
	d renderDriver
}

func (s ledgerEdges) record(ctx context.Context, edges []*storage.Edge) {
	for _, e := range edges {
		if e == nil {
			continue
		}
		s.d.ledger.AddEdge(e.ID)
		s.d.releaseEndpoint(ctx, e.FromType, e.FromID)
		s.d.releaseEndpoint(ctx, e.ToType, e.ToID)
	}
}

func (s ledgerEdges) ListFrom(ctx context.Context, fromType, fromID string) ([]*storage.Edge, error) {
	edges, err := s.EdgeStore.ListFrom(ctx, fromType, fromID)
	s.record(ctx, edges)
	return edges, err
}

func (s ledgerEdges) ListTo(ctx context.Context, toType, toID string) ([]*storage.Edge, error) {
	edges, err := s.EdgeStore.ListTo(ctx, toType, toID)
	s.record(ctx, edges)
	return edges, err
}

// ledgerProximity releases both objects of every neighbour pair
// through their scoped reads.
type ledgerProximity struct {
	storage.ProximityStore
	d renderDriver
}

func (s ledgerProximity) record(ctx context.Context, scores []*storage.ProximityScore) {
	for _, p := range scores {
		if p != nil {
			s.d.releaseObject(ctx, p.ObjectA)
			s.d.releaseObject(ctx, p.ObjectB)
		}
	}
}

func (s ledgerProximity) GetNeighbors(ctx context.Context, objectID string, limit int) ([]*storage.ProximityScore, error) {
	scores, err := s.ProximityStore.GetNeighbors(ctx, objectID, limit)
	s.record(ctx, scores)
	return scores, err
}

func (s ledgerProximity) GetNeighborsAbove(ctx context.Context, objectID string, threshold float64) ([]*storage.ProximityScore, error) {
	scores, err := s.ProximityStore.GetNeighborsAbove(ctx, objectID, threshold)
	s.record(ctx, scores)
	return scores, err
}

func (s ledgerProximity) Get(ctx context.Context, objectA, objectB string) (*storage.ProximityScore, error) {
	p, err := s.ProximityStore.Get(ctx, objectA, objectB)
	if p != nil {
		s.record(ctx, []*storage.ProximityScore{p})
	}
	return p, err
}

// ledgerAliases releases the object of every alias through its scoped
// read.
type ledgerAliases struct {
	storage.AliasStore
	d renderDriver
}

func (s ledgerAliases) Resolve(ctx context.Context, alias, profile string) (string, error) {
	id, err := s.AliasStore.Resolve(ctx, alias, profile)
	if err == nil {
		s.d.releaseObject(ctx, id)
	}
	return id, err
}

func (s ledgerAliases) List(ctx context.Context, filter storage.AliasFilter) ([]*storage.Alias, error) {
	as, err := s.AliasStore.List(ctx, filter)
	for _, a := range as {
		if a != nil {
			s.d.releaseObject(ctx, a.ObjectID)
		}
	}
	return as, err
}

// RenderedReads lists the ObjectStore methods the scoped driver
// renders the results of (tests).
func RenderedReads() []string { return append([]string(nil), renderedReads...) }

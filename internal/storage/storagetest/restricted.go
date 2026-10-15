package storagetest

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/embeddings/registry"
	"github.com/ideacrafterslabs/ctxt/internal/mentions"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blobref"
)

// The restricted fixture: the viewer sees active notes; contacts, the
// inbox note and everything derived only from them are hidden.
const (
	restrictedVisibleType = "note"
	restrictedHiddenType  = "contact"
	restrictedPrincipal   = "prn_restricted"
	restrictedBlobVisible = "sha256-restricted-visible-blob"
	restrictedBlobHidden  = "sha256-restricted-hidden-blob"
	restrictedModel       = "restricted-conf-3"
)

// restrictedFixture is what RestrictedViewerConformance seeds.
type restrictedFixture struct {
	// visible and visible2 are active notes the viewer sees; hidden is a
	// contact it does not; inbox is a note in the inbox, hidden by status.
	visible, visible2, hidden, inbox *storage.KnowledgeObject
	// mentioned is a placeholder entity both visible notes mention;
	// secret one only the hidden contact mentions; registry one a
	// registry served.
	mentioned, secret, registry *storage.Entity
	// edges by name.
	edges map[string]*storage.Edge
}

// RestrictedViewerConformance pins what a restricted viewer's scoped
// reads answer over drv (a fresh database), on every store the viewer's
// predicate reaches (ADR-080 §4, §6, §7): visible rows only, exact
// totals and counts, a hidden row as ErrNotFound, edges and backlinks
// kept to visible objects, placeholder entities only while a visible
// object mentions them, neighbours and aliases of visible objects
// only, blobs of visible objects only, per-principal rows as the
// viewer's own, full-text on the masked index and vectors on the
// masked variant over visible objects, widening until the asked-for
// number of visible hits.
func RestrictedViewerConformance(t *testing.T, drv storage.StorageDriver) {
	t.Helper()
	f := seedRestricted(t, drv)
	viewer := viewerOfType(restrictedVisibleType, restrictedPrincipal)
	scoped := storage.ScopedDriver(viewer, drv)

	t.Run("objects", func(t *testing.T) { restrictedConfObjects(t, scoped, f) })
	t.Run("full-text search on the masked index", func(t *testing.T) { restrictedConfFTS(t, scoped, f) })
	t.Run("edges keep both ends visible", func(t *testing.T) { restrictedConfEdges(t, scoped, f) })
	t.Run("edges hide entity ends without the capability", func(t *testing.T) {
		noEntities := viewer
		noEntities.noEntities = true
		restrictedConfEdgesNoEntities(t, storage.ScopedDriver(noEntities, drv), f)
	})
	t.Run("entities follow visible mentions", func(t *testing.T) { restrictedConfEntities(t, scoped, f) })
	t.Run("entities need the capability", func(t *testing.T) {
		noEntities := viewer
		noEntities.noEntities = true
		restrictedConfNoEntities(t, storage.ScopedDriver(noEntities, drv), f)
	})
	t.Run("proximity keeps visible pairs", func(t *testing.T) { restrictedConfProximity(t, scoped, f) })
	t.Run("aliases of visible objects", func(t *testing.T) { restrictedConfAliases(t, scoped, f) })
	t.Run("blobs of visible objects", func(t *testing.T) { restrictedConfBlobs(t, scoped, drv) })
	t.Run("per-principal rows", func(t *testing.T) { restrictedConfPrincipalRows(t, scoped, drv) })
	if edrv, ok := drv.(EmbeddingConformanceDriver); ok {
		t.Run("vectors over visible objects", func(t *testing.T) { restrictedConfVectors(t, scoped, edrv, f) })
	}
}

// seedRestricted writes the fixture through drv.
func seedRestricted(t *testing.T, drv storage.StorageDriver) *restrictedFixture {
	t.Helper()
	ctx := context.Background()
	now := fixtureTime()
	f := &restrictedFixture{edges: map[string]*storage.Edge{}}

	mention := func(slug string) *storage.Entity {
		return &storage.Entity{Slug: slug, Title: slug, Namespace: "person", CreatedAt: now, UpdatedAt: now}
	}
	f.mentioned = mention("person/mentioned")
	f.secret = mention("person/secret")
	f.registry = &storage.Entity{
		Slug: "org/registry", Title: "Registry org", Namespace: "org",
		RegistryURL: "https://registry.example", CreatedAt: now, UpdatedAt: now,
	}
	for _, e := range []*storage.Entity{f.mentioned, f.secret, f.registry} {
		if err := drv.Entities().Upsert(ctx, e); err != nil {
			t.Fatalf("seed entity %s: %v", e.Slug, err)
		}
	}

	obj := func(id, typ, text, hash, sourceKey string) *storage.KnowledgeObject {
		o := searchFixtureObject(id, typ, text)
		o.ContentHash = hash
		o.SourceKey = sourceKey
		o.Tags = []storage.Tag{{Label: "restricted"}}
		return o
	}
	f.visible = obj("rv-visible", restrictedVisibleType, "visible note about lanterns", "sha256:rv-visible", "src:rv-visible")
	f.visible.RawContent = blobref.For(restrictedBlobVisible)
	f.visible2 = obj("rv-visible-2", restrictedVisibleType, "second visible note about lanterns", "sha256:rv-visible-2", "src:rv-visible-2")
	f.hidden = obj("rv-hidden", restrictedHiddenType, "hidden contact about kraken", "sha256:rv-hidden", "src:rv-hidden")
	f.hidden.RawContent = blobref.For(restrictedBlobHidden)
	f.inbox = obj("rv-inbox", restrictedVisibleType, "inbox note about lanterns", "sha256:rv-inbox", "src:rv-inbox")
	f.inbox.Status = "inbox"
	for _, o := range []*storage.KnowledgeObject{f.visible, f.visible2, f.hidden, f.inbox} {
		if err := drv.Objects().Create(ctx, o); err != nil {
			t.Fatalf("seed object %s: %v", o.ID, err)
		}
	}
	for _, key := range []string{restrictedBlobVisible, restrictedBlobHidden} {
		if err := drv.Blobs().Put(ctx, key, strings.NewReader("blob "+key), storage.BlobMeta{}); err != nil {
			t.Fatalf("seed blob %s: %v", key, err)
		}
	}

	target := func(e *storage.Entity) string {
		u, _ := mentions.Parse("ctxt://entity/" + e.Slug)
		return storage.MentionTarget(u)
	}
	edge := func(name, fromType, fromID, toType, toID, kind string) {
		e := &storage.Edge{
			ID: "rv-edge-" + name, FromType: fromType, FromID: fromID, ToType: toType, ToID: toID,
			EdgeType: kind, Weight: 1, CreatedAt: now,
		}
		if err := drv.Edges().Create(ctx, e); err != nil {
			t.Fatalf("seed edge %s: %v", name, err)
		}
		f.edges[name] = e
	}
	// Mentions: both visible notes and the hidden contact mention
	// "mentioned"; only the hidden contact mentions "secret".
	edge("visible-mentions", "object", f.visible.ID, "entity", target(f.mentioned), storage.EdgeTypeMentions)
	edge("visible2-mentions", "object", f.visible2.ID, "entity", target(f.mentioned), storage.EdgeTypeMentions)
	edge("hidden-mentions", "object", f.hidden.ID, "entity", target(f.mentioned), storage.EdgeTypeMentions)
	edge("hidden-mentions-secret", "object", f.hidden.ID, "entity", target(f.secret), storage.EdgeTypeMentions)
	edge("mentioned-in-visible", "entity", target(f.mentioned), "object", f.visible.ID, storage.EdgeTypeMentionedIn)
	edge("mentioned-in-hidden", "entity", target(f.mentioned), "object", f.hidden.ID, storage.EdgeTypeMentionedIn)
	// Links: visible → hidden, hidden → visible, visible → visible2.
	edge("visible-links-hidden", "object", f.visible.ID, "object", f.hidden.ID, "references")
	edge("hidden-links-visible", "object", f.hidden.ID, "object", f.visible.ID, "references")
	edge("visible-links-visible2", "object", f.visible.ID, "object", f.visible2.ID, "references")

	for _, p := range []*storage.ProximityScore{
		{ObjectA: f.visible.ID, ObjectB: f.visible2.ID, Score: 0.9, ComputedAt: now},
		{ObjectA: f.visible.ID, ObjectB: f.hidden.ID, Score: 0.95, ComputedAt: now},
	} {
		if err := drv.Proximity().Put(ctx, p); err != nil {
			t.Fatalf("seed proximity: %v", err)
		}
	}
	for _, a := range []*storage.Alias{
		{Alias: "rv-alias-visible", ObjectID: f.visible.ID, Scope: "global", CreatedAt: now, UpdatedAt: now},
		{Alias: "rv-alias-hidden", ObjectID: f.hidden.ID, Scope: "global", CreatedAt: now, UpdatedAt: now},
	} {
		if err := drv.Aliases().Create(ctx, a); err != nil {
			t.Fatalf("seed alias %s: %v", a.Alias, err)
		}
	}
	return f
}

// rvObjectIDs returns the IDs of objs, sorted.
func rvObjectIDs(objs []*storage.KnowledgeObject) []string {
	out := make([]string, 0, len(objs))
	for _, o := range objs {
		out = append(out, o.ID)
	}
	slices.Sort(out)
	return out
}

// rvEdgeIDs returns the IDs of edges.
func rvEdgeIDs(edges []*storage.Edge) []string {
	out := make([]string, 0, len(edges))
	for _, e := range edges {
		out = append(out, e.ID)
	}
	slices.Sort(out)
	return out
}

func restrictedConfObjects(t *testing.T, scoped storage.StorageDriver, f *restrictedFixture) {
	ctx := context.Background()
	objects := scoped.Objects()

	got, err := objects.Get(ctx, f.visible.ID)
	if err != nil || got == nil || got.ID != f.visible.ID {
		t.Fatalf("Get(visible) = %v, %v", got, err)
	}
	for _, hidden := range []*storage.KnowledgeObject{f.hidden, f.inbox} {
		if _, err := objects.Get(ctx, hidden.ID); !errors.Is(err, storage.ErrNotFound) {
			t.Errorf("Get(%s) = %v, want ErrNotFound", hidden.ID, err)
		}
	}
	if _, err := objects.Get(ctx, "rv-missing"); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("Get(missing) = %v, want ErrNotFound", err)
	}

	restrictedConfObjectLists(t, objects, f)
	restrictedConfObjectLookups(t, objects, f)
}

// restrictedConfObjectLists checks List, ListBySQL and CountByType
// answer visible objects with exact totals.
func restrictedConfObjectLists(t *testing.T, objects storage.ObjectStore, f *restrictedFixture) {
	ctx := context.Background()
	want := []string{f.visible.ID, f.visible2.ID}
	for _, filter := range []storage.ObjectFilter{{}, {Status: storage.ObjectStatusAll}, {Tag: "restricted"}} {
		objs, total, err := objects.List(ctx, filter)
		if err != nil {
			t.Fatalf("List(%+v): %v", filter, err)
		}
		if got := rvObjectIDs(objs); !slices.Equal(got, want) || total != len(want) {
			t.Errorf("List(%+v) = %v total %d, want %v total %d", filter, got, total, want, len(want))
		}
	}
	// The inbox is hidden even when asked for by status.
	if objs, total, err := objects.List(ctx, storage.ObjectFilter{Status: "inbox"}); err != nil || len(objs) != 0 || total != 0 {
		t.Errorf("List(inbox) = %v total %d err %v, want nothing", rvObjectIDs(objs), total, err)
	}
	objs, total, err := objects.ListBySQL(ctx, "id LIKE ?", []any{"rv-%"}, storage.ObjectFilter{})
	if err != nil {
		t.Fatalf("ListBySQL: %v", err)
	}
	if got := rvObjectIDs(objs); !slices.Equal(got, want) || total != len(want) {
		t.Errorf("ListBySQL = %v total %d, want %v", got, total, want)
	}
	counts, err := objects.CountByType(ctx)
	if err != nil {
		t.Fatalf("CountByType: %v", err)
	}
	if counts[restrictedVisibleType] != 2 || counts[restrictedHiddenType] != 0 || len(counts) != 1 {
		t.Errorf("CountByType = %v, want {%s: 2}", counts, restrictedVisibleType)
	}
}

// restrictedConfObjectLookups checks the content-hash and source-key
// lookups answer visible objects only.
func restrictedConfObjectLookups(t *testing.T, objects storage.ObjectStore, f *restrictedFixture) {
	ctx := context.Background()
	if got, err := objects.GetByContentHash(ctx, f.visible.ContentHash, f.visible.ProfileID); err != nil || got == nil {
		t.Errorf("GetByContentHash(visible) = %v, %v", got, err)
	}
	if got, err := objects.GetByContentHash(ctx, f.hidden.ContentHash, f.hidden.ProfileID); err != nil || got != nil {
		t.Errorf("GetByContentHash(hidden) = %v, %v, want nil, nil", got, err)
	}
	if got, err := objects.GetBySourceKey(ctx, f.visible.SourceKey); err != nil || got == nil {
		t.Errorf("GetBySourceKey(visible) = %v, %v", got, err)
	}
	if got, err := objects.GetBySourceKey(ctx, f.hidden.SourceKey); err != nil || got != nil {
		t.Errorf("GetBySourceKey(hidden) = %v, %v, want nil, nil", got, err)
	}
}

func restrictedConfFTS(t *testing.T, scoped storage.StorageDriver, f *restrictedFixture) {
	ctx := context.Background()
	objs, err := scoped.Objects().FTSSearch(ctx, "lanterns", storage.ObjectFilter{Limit: 10})
	if err != nil {
		t.Fatalf("FTSSearch(lanterns): %v", err)
	}
	if got := rvObjectIDs(objs); !slices.Equal(got, []string{f.visible.ID, f.visible2.ID}) {
		t.Errorf("FTSSearch(lanterns) = %v, want the two visible notes", got)
	}
	objs, err = scoped.Objects().FTSSearch(ctx, "kraken", storage.ObjectFilter{Limit: 10})
	if err != nil {
		t.Fatalf("FTSSearch(kraken): %v", err)
	}
	if len(objs) != 0 {
		t.Errorf("FTSSearch(kraken) = %v, want nothing: the term is in a hidden object only", rvObjectIDs(objs))
	}
	res, err := scoped.Objects().FTSSearchNodeAware(ctx, "lanterns", storage.ObjectFilter{Limit: 10}, storage.NodeAwareFilter{})
	if err != nil || len(res) != 2 {
		t.Errorf("FTSSearchNodeAware(lanterns) = %d results, %v, want 2", len(res), err)
	}
}

func restrictedConfEdges(t *testing.T, scoped storage.StorageDriver, f *restrictedFixture) {
	ctx := context.Background()
	edges := scoped.Edges()

	from, err := edges.ListFrom(ctx, "object", f.visible.ID)
	if err != nil {
		t.Fatalf("ListFrom(visible): %v", err)
	}
	want := []string{f.edges["visible-links-visible2"].ID, f.edges["visible-mentions"].ID}
	if got := rvEdgeIDs(from); !slices.Equal(got, want) {
		t.Errorf("ListFrom(visible) = %v, want %v (the link to the hidden object is gone)", got, want)
	}
	to, err := edges.ListTo(ctx, "object", f.visible.ID)
	if err != nil {
		t.Fatalf("ListTo(visible): %v", err)
	}
	want = []string{f.edges["mentioned-in-visible"].ID}
	if got := rvEdgeIDs(to); !slices.Equal(got, want) {
		t.Errorf("ListTo(visible) = %v, want %v (the link from the hidden object is gone)", got, want)
	}
	if got, err := edges.ListFrom(ctx, "object", f.hidden.ID); err != nil || len(got) != 0 {
		t.Errorf("ListFrom(hidden) = %v, %v, want nothing", rvEdgeIDs(got), err)
	}
	// Backlinks of the entity: the two visible sources only.
	target := f.edges["visible-mentions"].ToID
	back, err := edges.ListTo(ctx, "entity", target)
	if err != nil {
		t.Fatalf("ListTo(entity): %v", err)
	}
	want = []string{f.edges["visible-mentions"].ID, f.edges["visible2-mentions"].ID}
	if got := rvEdgeIDs(back); !slices.Equal(got, want) {
		t.Errorf("ListTo(entity) = %v, want %v", got, want)
	}
	n, err := edges.CountMentionsTo(ctx, "entity", target)
	if err != nil || n != 2 {
		t.Errorf("CountMentionsTo(entity) = %d, %v, want 2 (visible sources only)", n, err)
	}
	n, err = edges.CountMentionsTo(ctx, "object", f.visible.ID)
	if err != nil || n != 1 {
		t.Errorf("CountMentionsTo(visible) = %d, %v, want 1 (the mentioned_in edge; the hidden link is gone)", n, err)
	}
	// Related: visible2 shares the entity; the hidden contact shares it
	// too but stays out.
	rel, err := edges.RelatedObjectIDs(ctx, f.visible.ID, 2, 10)
	if err != nil {
		t.Fatalf("RelatedObjectIDs: %v", err)
	}
	slices.Sort(rel)
	if !slices.Equal(rel, []string{f.visible2.ID}) {
		t.Errorf("RelatedObjectIDs(visible) = %v, want [%s]", rel, f.visible2.ID)
	}
	if rel, err := edges.RelatedObjectIDs(ctx, f.hidden.ID, 1, 10); err != nil || len(rel) != 0 {
		t.Errorf("RelatedObjectIDs(hidden seed) = %v, %v, want nothing", rel, err)
	}
}

func restrictedConfEdgesNoEntities(t *testing.T, scoped storage.StorageDriver, f *restrictedFixture) {
	ctx := context.Background()
	from, err := scoped.Edges().ListFrom(ctx, "object", f.visible.ID)
	if err != nil {
		t.Fatalf("ListFrom(visible): %v", err)
	}
	want := []string{f.edges["visible-links-visible2"].ID}
	if got := rvEdgeIDs(from); !slices.Equal(got, want) {
		t.Errorf("ListFrom(visible) without entities = %v, want %v", got, want)
	}
	if n, err := scoped.Edges().CountMentionsTo(ctx, "entity", f.edges["visible-mentions"].ToID); err != nil || n != 0 {
		t.Errorf("CountMentionsTo(entity) without entities = %d, %v, want 0", n, err)
	}
}

func restrictedConfEntities(t *testing.T, scoped storage.StorageDriver, f *restrictedFixture) {
	ctx := context.Background()
	entities := scoped.Entities()
	for _, visible := range []*storage.Entity{f.mentioned, f.registry} {
		if got, err := entities.Get(ctx, visible.Slug); err != nil || got == nil || got.Slug != visible.Slug {
			t.Errorf("Get(%s) = %v, %v", visible.Slug, got, err)
		}
		if got, err := entities.Resolve(ctx, visible.Slug); err != nil || got == nil {
			t.Errorf("Resolve(%s) = %v, %v", visible.Slug, got, err)
		}
	}
	if _, err := entities.Get(ctx, f.secret.Slug); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("Get(secret) = %v, want ErrNotFound: only a hidden object mentions it", err)
	}
	if _, err := entities.Resolve(ctx, f.secret.Slug); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("Resolve(secret) = %v, want ErrNotFound", err)
	}
	list, err := entities.List(ctx, storage.EntityFilter{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	slugs := make([]string, 0, len(list))
	for _, e := range list {
		slugs = append(slugs, e.Slug)
	}
	slices.Sort(slugs)
	if want := []string{f.registry.Slug, f.mentioned.Slug}; !slices.Equal(slugs, want) {
		t.Errorf("List = %v, want %v", slugs, want)
	}
}

func restrictedConfNoEntities(t *testing.T, scoped storage.StorageDriver, f *restrictedFixture) {
	ctx := context.Background()
	for _, e := range []*storage.Entity{f.mentioned, f.registry, f.secret} {
		if _, err := scoped.Entities().Get(ctx, e.Slug); !errors.Is(err, storage.ErrNotFound) {
			t.Errorf("Get(%s) without the entities capability = %v, want ErrNotFound", e.Slug, err)
		}
	}
	if list, err := scoped.Entities().List(ctx, storage.EntityFilter{}); err != nil || len(list) != 0 {
		t.Errorf("List without the entities capability = %d entities, %v, want none", len(list), err)
	}
}

func restrictedConfProximity(t *testing.T, scoped storage.StorageDriver, f *restrictedFixture) {
	ctx := context.Background()
	neighbours, err := scoped.Proximity().GetNeighbors(ctx, f.visible.ID, 10)
	if err != nil {
		t.Fatalf("GetNeighbors: %v", err)
	}
	if len(neighbours) != 1 || neighbours[0].ObjectB != f.visible2.ID && neighbours[0].ObjectA != f.visible2.ID {
		t.Errorf("GetNeighbors(visible) = %+v, want the visible pair only", neighbours)
	}
	above, err := scoped.Proximity().GetNeighborsAbove(ctx, f.visible.ID, 0.5)
	if err != nil || len(above) != 1 {
		t.Errorf("GetNeighborsAbove(visible) = %+v, %v, want one pair", above, err)
	}
	if p, err := scoped.Proximity().Get(ctx, f.visible.ID, f.hidden.ID); err != nil || p != nil {
		t.Errorf("Get(visible, hidden) = %+v, %v, want nil, nil", p, err)
	}
	if p, err := scoped.Proximity().Get(ctx, f.visible.ID, f.visible2.ID); err != nil || p == nil {
		t.Errorf("Get(visible, visible2) = %+v, %v, want the pair", p, err)
	}
}

func restrictedConfAliases(t *testing.T, scoped storage.StorageDriver, f *restrictedFixture) {
	ctx := context.Background()
	if id, err := scoped.Aliases().Resolve(ctx, "rv-alias-visible", ""); err != nil || id != f.visible.ID {
		t.Errorf("Resolve(visible alias) = %q, %v", id, err)
	}
	if _, err := scoped.Aliases().Resolve(ctx, "rv-alias-hidden", ""); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("Resolve(hidden alias) = %v, want ErrNotFound", err)
	}
	list, err := scoped.Aliases().List(ctx, storage.AliasFilter{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	for _, a := range list {
		if a.ObjectID == f.hidden.ID {
			t.Errorf("List returned the hidden object's alias %q", a.Alias)
		}
	}
	if len(list) == 0 {
		t.Error("List returned nothing, want the visible alias")
	}
}

func restrictedConfBlobs(t *testing.T, scoped, drv storage.StorageDriver) {
	ctx := context.Background()
	rc, _, err := scoped.Blobs().Get(ctx, restrictedBlobVisible)
	if err != nil {
		t.Fatalf("Get(visible blob): %v", err)
	}
	body, _ := io.ReadAll(rc)
	_ = rc.Close()
	if string(body) != "blob "+restrictedBlobVisible {
		t.Errorf("Get(visible blob) = %q", body)
	}
	_, _, err = scoped.Blobs().Get(ctx, restrictedBlobHidden)
	if !errors.Is(err, storage.ErrBlobNotFound) || !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("Get(hidden blob) = %v, want ErrBlobNotFound and ErrNotFound", err)
	}
	if ok, err := scoped.Blobs().Exists(ctx, restrictedBlobHidden); err != nil || ok {
		t.Errorf("Exists(hidden blob) = %v, %v, want false", ok, err)
	}
	if ok, err := scoped.Blobs().Exists(ctx, restrictedBlobVisible); err != nil || !ok {
		t.Errorf("Exists(visible blob) = %v, %v, want true", ok, err)
	}
	if _, err := scoped.Blobs().URL(ctx, restrictedBlobHidden); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("URL(hidden blob) = %v, want ErrNotFound", err)
	}
	all, err := drv.Blobs().List(ctx, "sha256-restricted-")
	if err != nil {
		t.Fatalf("driver List: %v", err)
	}
	visible, err := scoped.Blobs().List(ctx, "sha256-restricted-")
	if err != nil {
		t.Fatalf("scoped List: %v", err)
	}
	if len(all) != 2 || len(visible) != 1 || visible[0].Key != restrictedBlobVisible {
		t.Errorf("List = %d of %d blobs (%+v), want the visible one only", len(visible), len(all), visible)
	}
}

func restrictedConfPrincipalRows(t *testing.T, scoped, drv storage.StorageDriver) {
	ctx := context.Background()
	now := fixtureTime()
	owner := &storage.SavedSearch{ID: "rv-ss-owner", Name: "rv-owner-search", Query: "q", CreatedAt: now, UpdatedAt: now}
	err := drv.SavedSearches().Create(ctx, owner)
	if errors.Is(err, storage.ErrNotImplemented) {
		t.Skipf("PARITY GAP: %v", err)
	}
	if err != nil {
		t.Fatalf("owner Create: %v", err)
	}
	mine := &storage.SavedSearch{ID: "rv-ss-mine", Name: "rv-my-search", Query: "q", CreatedAt: now, UpdatedAt: now}
	if err := scoped.SavedSearches().Create(ctx, mine); err != nil {
		t.Fatalf("restricted Create: %v", err)
	}
	if mine.OwnerPrincipal != restrictedPrincipal {
		t.Errorf("restricted Create stored owner %q, want %q", mine.OwnerPrincipal, restrictedPrincipal)
	}
	list, err := scoped.SavedSearches().List(ctx, storage.SavedSearchFilter{})
	if err != nil || len(list) != 1 || list[0].ID != mine.ID {
		t.Errorf("restricted List = %+v, %v, want the viewer's own search", list, err)
	}
	if got, err := scoped.SavedSearches().GetByName(ctx, owner.Name); err != nil || got != nil {
		t.Errorf("GetByName(owner's) = %+v, %v, want nil, nil", got, err)
	}
	if err := scoped.SavedSearches().Delete(ctx, owner.Name); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("Delete(owner's) = %v, want ErrNotFound", err)
	}
	if got, err := drv.SavedSearches().GetByName(ctx, owner.Name); err != nil || got == nil {
		t.Errorf("the owner's search is gone: %v, %v", got, err)
	}
	restrictedConfPrincipalQueues(t, scoped, drv)
}

// restrictedConfPrincipalQueues checks search history and the
// resurfacing queue are read and written as the viewer's principal.
func restrictedConfPrincipalQueues(t *testing.T, scoped, drv storage.StorageDriver) {
	ctx := context.Background()
	now := fixtureTime()
	if err := drv.SearchHistory().Append(ctx, &storage.SearchHistoryEntry{ID: "rv-sh-owner", Query: "owner q", SearchedAt: now}); err != nil {
		t.Fatalf("owner Append: %v", err)
	}
	if err := scoped.SearchHistory().Append(ctx, &storage.SearchHistoryEntry{ID: "rv-sh-mine", Query: "my q", SearchedAt: now}); err != nil {
		t.Fatalf("restricted Append: %v", err)
	}
	hist, err := scoped.SearchHistory().List(ctx, storage.SearchHistoryFilter{})
	if err != nil || len(hist) != 1 || hist[0].ID != "rv-sh-mine" {
		t.Errorf("restricted history List = %+v, %v, want the viewer's own entry", hist, err)
	}

	if err := drv.Resurfacing().Upsert(ctx, &storage.ResurfacingEntry{ID: "rv-rs-owner", ObjectID: "rv-visible", Score: 1, CreatedAt: now}); err != nil {
		t.Fatalf("owner Upsert: %v", err)
	}
	if err := scoped.Resurfacing().Upsert(ctx, &storage.ResurfacingEntry{ID: "rv-rs-mine", ObjectID: "rv-visible", ProfileID: "p", Score: 1, CreatedAt: now}); err != nil {
		t.Fatalf("restricted Upsert: %v", err)
	}
	queue, err := scoped.Resurfacing().List(ctx, storage.ResurfacingFilter{})
	if err != nil || len(queue) != 1 || queue[0].ID != "rv-rs-mine" {
		t.Errorf("restricted resurfacing List = %+v, %v, want the viewer's own entry", queue, err)
	}
}

// restrictedConfVectors seeds restrictedModel with one visible object
// far from the query, one visible object whose masked row is close to
// it, and more hidden objects closer than both than a round fetches,
// then checks the scoped searches rank only visible objects, through
// the masked variant, and widen past the hidden ones.
func restrictedConfVectors(t *testing.T, scoped storage.StorageDriver, drv EmbeddingConformanceDriver, f *restrictedFixture) {
	ctx := context.Background()
	dialect := storage.SQLDialectSQLite
	if d, ok := drv.(interface{ SQLDialect() string }); ok && d.SQLDialect() == storage.SQLDialectPostgres {
		dialect = storage.SQLDialectPostgres
	}
	reg := registry.NewFor(drv.DB(), dialect)
	if err := reg.Register(ctx, registry.Model{ModelID: restrictedModel, Provider: "conformance", Dimension: 3}, false); err != nil {
		t.Fatalf("register model: %v", err)
	}
	if err := drv.Embeddings().EnsureIndex(ctx, storage.EmbeddingModelSpec{ModelID: restrictedModel, Provider: "conformance", Dimension: 3}); err != nil {
		t.Fatalf("EnsureIndex: %v", err)
	}
	query := []float32{1, 0, 0}
	put := func(id string, raw, masked []float32) {
		vs := []storage.ObjectVector{{ModelID: restrictedModel, ChunkIdx: 0, Vector: raw}}
		if masked != nil {
			vs = append(vs, storage.ObjectVector{ModelID: restrictedModel, ChunkIdx: 0, Variant: storage.EmbeddingVariantMasked, Vector: masked})
		}
		if err := drv.Embeddings().Put(ctx, id, vs); err != nil {
			t.Fatalf("Put(%s): %v", id, err)
		}
	}
	// visible: raw far from the query, masked close to it; visible2 far.
	put(f.visible.ID, []float32{0, 0, 1}, []float32{0.9, 0.1, 0})
	put(f.visible2.ID, []float32{0, 1, 0}, nil)
	// Hidden objects, all closer to the query than any visible raw row:
	// more of them than the first round fetches for TopK 1.
	const hiddenCount = 30
	for i := 0; i < hiddenCount; i++ {
		id := fmt.Sprintf("rv-vec-hidden-%02d", i)
		if err := drv.Objects().Create(ctx, searchFixtureObject(id, restrictedHiddenType, "hidden vector "+id)); err != nil {
			t.Fatalf("create %s: %v", id, err)
		}
		put(id, []float32{1, 0.01 * float32(i), 0}, nil)
	}

	hits, err := scoped.Embeddings().Search(ctx, storage.VectorQuery{ModelID: restrictedModel, Vector: query, TopK: 1})
	if err != nil {
		t.Fatalf("scoped Search: %v", err)
	}
	if len(hits) != 1 || hits[0].ObjectID != f.visible.ID {
		t.Fatalf("scoped Search(TopK 1) = %+v, want the visible object through its masked row, past %d closer hidden ones", hits, hiddenCount)
	}
	// Its distance is the masked row's, not the raw row's (which is
	// orthogonal to the query: distance 1).
	if hits[0].Distance > 0.5 {
		t.Errorf("scoped Search ranked the visible object at distance %v, want the masked row's (close)", hits[0].Distance)
	}
	hits, err = scoped.Embeddings().Search(ctx, storage.VectorQuery{ModelID: restrictedModel, Vector: query, TopK: 10})
	if err != nil {
		t.Fatalf("scoped Search: %v", err)
	}
	got := make([]string, 0, len(hits))
	for _, h := range hits {
		got = append(got, h.ObjectID)
	}
	if !slices.Equal(got, []string{f.visible.ID, f.visible2.ID}) {
		t.Errorf("scoped Search(TopK 10) = %v, want [%s %s]: every visible object, no hidden one", got, f.visible.ID, f.visible2.ID)
	}
	objs, err := scoped.Objects().VectorSearch(ctx, storage.VectorQuery{ModelID: restrictedModel, Vector: query}, storage.ObjectFilter{Limit: 5})
	if err != nil {
		t.Fatalf("scoped VectorSearch: %v", err)
	}
	if got := rvObjectIDs(objs); !slices.Equal(got, []string{f.visible.ID, f.visible2.ID}) {
		t.Errorf("scoped VectorSearch = %v, want the two visible objects", got)
	}
	_ = time.Now
}

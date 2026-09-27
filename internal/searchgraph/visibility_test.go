package searchgraph

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// hiddenSlugs are sample-fixture entities a visibility predicate hides.
// search/bm25 is the only entity kb-rrf and kb-bm25 share; db/pgvector is
// one of the two kb-pgvec and kb-embed share. db/sqlite has no stored
// record, so the predicate sees it as nil.
var hiddenSlugs = []string{"search/bm25", "db/pgvector", "db/sqlite"}

func hideSlugs(slugs ...string) EntityVisibility {
	return func(_ context.Context, slug string, _ *storage.Entity) bool {
		return !slices.Contains(slugs, slug)
	}
}

// withoutMentions drops every stored mention row targeting slugs: the
// store as it would be had the hidden entities never been mentioned.
func withoutMentions(f *fakeStore, slugs ...string) {
	for from, rows := range f.edges {
		f.edges[from] = slices.DeleteFunc(slices.Clone(rows), func(e *storage.Edge) bool {
			return e.ToType == "entity" && e.EdgeType == RelMentions && slices.Contains(slugs, e.ToID)
		})
	}
	for _, s := range slugs {
		delete(f.entities, s)
	}
}

// TestHiddenEntitiesLeaveNoTrace pins the gate's contract: a document
// built with hidden entities is byte-identical to one built from a store
// that never held their mentions. No node, edge, weight, count, mention
// count or truncation flag may differ, whatever the caps.
func TestHiddenEntitiesLeaveNoTrace(t *testing.T) {
	for name, opts := range map[string]Options{
		"defaults":           {},
		"similar":            {Similar: true},
		"entity cap":         {MaxNodes: 13},
		"entity + edge caps": {MaxNodes: 13, MaxEdges: 20, Similar: true},
		"edge cap":           {MaxEdges: 25},
	} {
		t.Run(name, func(t *testing.T) {
			tr, f := sampleFixture()
			gated := opts
			gated.EntityVisible = hideSlugs(hiddenSlugs...)
			got := encode(t, build(t, tr, f, gated))

			refTr, ref := sampleFixture()
			withoutMentions(ref, hiddenSlugs...)
			want := encode(t, build(t, refTr, ref, opts))

			if !bytes.Equal(got, want) {
				t.Errorf("gated document differs from one without the hidden mentions:\n got: %s\nwant: %s", got, want)
			}
			for _, s := range hiddenSlugs {
				if bytes.Contains(got, []byte(s)) {
					t.Errorf("document names hidden entity %s", s)
				}
			}
			validateJGF(t, got)
			checkContract(t, got)
		})
	}
}

// TestHiddenEntitiesChangeWeights guards the reference comparison above:
// the hidden entities do contribute to the ungated document, so equality
// is not vacuous.
func TestHiddenEntitiesChangeWeights(t *testing.T) {
	tr, f := sampleFixture()
	open := build(t, tr, f, Options{})
	tr, f = sampleFixture()
	gated := build(t, tr, f, Options{EntityVisible: hideSlugs(hiddenSlugs...)})

	weight := func(doc *Document, a, b string) float64 {
		for _, e := range edgesBy(doc, RelCoMention) {
			if e.Source == a && e.Target == b {
				return *e.Metadata.Weight
			}
		}
		return 0
	}
	if w := weight(open, "obj:kb-embed", "obj:kb-pgvec"); w != 2 {
		t.Fatalf("ungated kb-embed/kb-pgvec weight = %v, want 2", w)
	}
	if w := weight(gated, "obj:kb-embed", "obj:kb-pgvec"); w != 1 {
		t.Errorf("gated kb-embed/kb-pgvec weight = %v, want 1 (db/pgvector hidden)", w)
	}
	if w := weight(gated, "obj:kb-bm25", "obj:kb-rrf"); w != 0 {
		t.Errorf("gated kb-bm25/kb-rrf co_mention weight = %v, want no edge (only search/bm25 shared)", w)
	}
	if open.Graph.Metadata.Counts.Entities-gated.Graph.Metadata.Counts.Entities != len(hiddenSlugs) {
		t.Errorf("entities %d -> %d, want %d fewer", open.Graph.Metadata.Counts.Entities,
			gated.Graph.Metadata.Counts.Entities, len(hiddenSlugs))
	}
}

// TestEntityVisibilityInputs checks what the predicate sees: each
// mentioned slug once, with its stored record or nil when the store has
// none, and one entity lookup per slug in total (labels reuse it).
func TestEntityVisibilityInputs(t *testing.T) {
	tr, f := sampleFixture()
	seen := map[string]*storage.Entity{}
	calls := 0
	build(t, tr, f, Options{EntityVisible: func(_ context.Context, slug string, ent *storage.Entity) bool {
		calls++
		seen[slug] = ent
		return true
	}})
	if calls != len(seen) {
		t.Errorf("predicate called %d times for %d slugs, want once each", calls, len(seen))
	}
	if ent, ok := seen["db/sqlite"]; !ok || ent != nil {
		t.Errorf("db/sqlite (no stored record) = %v, %v; want nil entity", ent, ok)
	}
	if ent := seen["search/rrf"]; ent == nil || ent.Title != "Reciprocal Rank Fusion" {
		t.Errorf("search/rrf entity = %+v, want the stored record", ent)
	}
	if f.getCalls != len(seen) {
		t.Errorf("entity lookups = %d, want %d (one per slug)", f.getCalls, len(seen))
	}
}

// TestEntityVisibilityLookupErrorFailsClosed: with a predicate set, an
// entity lookup error fails the build rather than guessing visibility.
func TestEntityVisibilityLookupErrorFailsClosed(t *testing.T) {
	tr, f := sampleFixture()
	f.getErr = errors.New("store down")
	_, err := Build(context.Background(), tr, f.source(), Options{EntityVisible: hideSlugs()})
	if err == nil {
		t.Fatal("Build succeeded despite an entity lookup error")
	}
}

// TestNoPredicateKeepsEveryEntity: the CLI passes no predicate and gets
// the ungated document, with the same lookups as before the gate existed.
func TestNoPredicateKeepsEveryEntity(t *testing.T) {
	tr, f := sampleFixture()
	doc := build(t, tr, f, Options{})
	if doc.Graph.Metadata.Counts.Entities != 7 {
		t.Errorf("entities = %d, want 7", doc.Graph.Metadata.Counts.Entities)
	}
	if f.getCalls != 7 {
		t.Errorf("entity lookups = %d, want 7 (kept entities only)", f.getCalls)
	}
}

// TestRequireEntityVisibility: a caller that demands a predicate and
// forgets it gets an error, never the ungated document.
func TestRequireEntityVisibility(t *testing.T) {
	tr, f := sampleFixture()
	doc, err := Build(context.Background(), tr, f.source(), Options{RequireEntityVisibility: true})
	if !errors.Is(err, ErrEntityVisibilityRequired) || doc != nil {
		t.Fatalf("Build = %v, %v; want nil, ErrEntityVisibilityRequired", doc, err)
	}
	if f.listCalls != 0 || f.getCalls != 0 {
		t.Errorf("store read before the policy check: %d lists, %d gets", f.listCalls, f.getCalls)
	}
	build(t, tr, f, Options{RequireEntityVisibility: true, EntityVisible: hideSlugs()})
}

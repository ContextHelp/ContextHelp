package servicetest

import (
	"bytes"
	"context"
	"slices"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/searchgraph"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Search graph gate corpus: entities SeedGraphGateCorpus stores and mentions.
const (
	// GraphOpenEntity lives in GraphOpenNamespace, which a gated caller
	// may see.
	GraphOpenEntity    = "gate/open"
	GraphOpenNamespace = "open.ns"
	// GraphSecretEntity lives in GraphSecretNamespace, which a gated
	// caller may not see.
	GraphSecretEntity    = "gate/secret"
	GraphSecretNamespace = "secret.ns"
	// GraphSecretTitle is GraphSecretEntity's display name; it must never
	// reach a gated document.
	GraphSecretTitle = "Classified Codename"
)

// SeedGraphGateCorpus stores SeedProfileCorpus plus two entities and
// their mentions: alpha-text mentions both, alpha-vec only the secret one
// (so the alpha pair shares nothing visible), beta-text the secret one.
func (f *HybridFixture) SeedGraphGateCorpus(t testing.TB) {
	t.Helper()
	ctx := context.Background()
	f.SeedProfileCorpus(t)
	now := time.Now().UTC().Truncate(time.Second)
	for slug, ns := range map[string]string{GraphOpenEntity: GraphOpenNamespace, GraphSecretEntity: GraphSecretNamespace} {
		title := "Open Topic"
		if slug == GraphSecretEntity {
			title = GraphSecretTitle
		}
		if err := f.Drv.Entities().Upsert(ctx, &storage.Entity{
			Slug: slug, Title: title, Namespace: ns, CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("upsert %s: %v", slug, err)
		}
	}
	for i, m := range [][2]string{
		{ProfileCorpusID("alpha", "text"), GraphOpenEntity},
		{ProfileCorpusID("alpha", "text"), GraphSecretEntity},
		{ProfileCorpusID("alpha", "vec"), GraphSecretEntity},
		{ProfileCorpusID("beta", "text"), GraphSecretEntity},
	} {
		if err := f.Drv.Edges().Create(ctx, &storage.Edge{
			ID: "gate-mention-" + string(rune('a'+i)), FromType: "object", FromID: m[0],
			ToType: "entity", ToID: m[1], EdgeType: searchgraph.RelMentions, Weight: 1, CreatedAt: now,
		}); err != nil {
			t.Fatalf("mention %s -> %s: %v", m[0], m[1], err)
		}
	}
}

// NamespaceVisibility is a gate stand-in: entities in hidden namespaces,
// and entities without a stored record, are invisible.
func NamespaceVisibility(hidden ...string) searchgraph.EntityVisibility {
	return func(_ context.Context, _ string, ent *storage.Entity) bool {
		return ent != nil && !slices.Contains(hidden, ent.Namespace)
	}
}

// RunSearchGraphGate asserts, on drv (a fresh database), that the search
// graph pipeline dpkms serves — Service.Find with Trace, then searchgraph.Build
// over the driver — scopes objects to the requested profile and drops a
// hidden entity from every part of the document, including the co_mention
// weight it would have created.
func RunSearchGraphGate(t *testing.T, drv storage.StorageDriver) {
	t.Helper()
	ctx := context.Background()
	f := NewHybridFixture(t, drv, true)
	f.SeedGraphGateCorpus(t)

	build := func(t *testing.T, opts searchgraph.Options) (*searchgraph.Document, []byte) {
		t.Helper()
		res, err := f.Svc.Find(ctx, service.FindRequest{
			Query: ProfileQuery, Profile: "alpha", Limit: 10, Trace: true,
		}, f.Sem)
		if err != nil {
			t.Fatalf("Find: %v", err)
		}
		if res.Trace == nil || res.Trace.Mode != service.SearchModeHybrid {
			t.Fatalf("trace = %+v, want a hybrid trace (vector leg ran)", res.Trace)
		}
		doc, err := searchgraph.Build(ctx, res.Trace, searchgraph.SourceFrom(drv), opts)
		if err != nil {
			t.Fatalf("Build: %v", err)
		}
		var buf bytes.Buffer
		if err := doc.Encode(&buf, false); err != nil {
			t.Fatalf("encode: %v", err)
		}
		return doc, buf.Bytes()
	}
	nodeIDs := func(doc *searchgraph.Document, kind string) []string {
		var ids []string
		for id, n := range doc.Graph.Nodes {
			if n.Metadata.Kind == kind {
				ids = append(ids, id)
			}
		}
		return sorted(ids)
	}
	coMentions := func(doc *searchgraph.Document) int {
		n := 0
		for _, e := range doc.Graph.Edges {
			if e.Relation == searchgraph.RelCoMention {
				n++
			}
		}
		return n
	}
	wantObjects := []string{
		searchgraph.ObjectNodePrefix + ProfileCorpusID("alpha", "text"),
		searchgraph.ObjectNodePrefix + ProfileCorpusID("alpha", "vec"),
	}

	t.Run("ungated", func(t *testing.T) {
		doc, _ := build(t, searchgraph.Options{})
		if got := nodeIDs(doc, searchgraph.KindObject); !equal(got, wantObjects) {
			t.Errorf("objects = %v, want %v (profile alpha only)", got, wantObjects)
		}
		wantEnts := []string{searchgraph.EntityNodePrefix + GraphOpenEntity, searchgraph.EntityNodePrefix + GraphSecretEntity}
		if got := nodeIDs(doc, searchgraph.KindEntity); !equal(got, wantEnts) {
			t.Errorf("entities = %v, want %v", got, wantEnts)
		}
		if n := coMentions(doc); n != 1 {
			t.Errorf("co_mention edges = %d, want 1 (the alpha pair shares the secret entity)", n)
		}
	})

	t.Run("gated", func(t *testing.T) {
		doc, raw := build(t, searchgraph.Options{
			RequireEntityVisibility: true,
			EntityVisible:           NamespaceVisibility(GraphSecretNamespace),
		})
		if got := nodeIDs(doc, searchgraph.KindObject); !equal(got, wantObjects) {
			t.Errorf("objects = %v, want %v (profile alpha only)", got, wantObjects)
		}
		wantEnts := []string{searchgraph.EntityNodePrefix + GraphOpenEntity}
		if got := nodeIDs(doc, searchgraph.KindEntity); !equal(got, wantEnts) {
			t.Errorf("entities = %v, want %v", got, wantEnts)
		}
		if n := coMentions(doc); n != 0 {
			t.Errorf("co_mention edges = %d, want 0 (only the hidden entity was shared)", n)
		}
		if c := doc.Graph.Metadata.Counts.Entities; c != 1 {
			t.Errorf("counts.entities = %d, want 1", c)
		}
		for _, secret := range []string{GraphSecretEntity, GraphSecretNamespace, GraphSecretTitle} {
			if bytes.Contains(raw, []byte(secret)) {
				t.Errorf("gated document contains %q", secret)
			}
		}
	})
}

package storagetest

import (
	"context"
	"reflect"
	"slices"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func objectEdge(id, from, typ, to string) *storage.Edge {
	return &storage.Edge{
		ID: id, FromType: "object", FromID: from, ToType: "object", ToID: to,
		EdgeType: typ, Weight: 1, Metadata: map[string]any{"context": "c-" + id},
		CreatedAt: fixtureTime(),
	}
}

// EdgeMetadataConformance pins that Edge.Metadata round-trips through
// Create, ListFrom and ListTo on every driver, and that an edge created
// without metadata reads back without any. Mention provenance
// (source_node, source_field) rides on it.
func EdgeMetadataConformance(t *testing.T, drv storage.StorageDriver) {
	t.Helper()
	ctx := context.Background()
	es := drv.Edges()

	meta := map[string]any{
		storage.ProvenanceSourceNode:  storage.NewNodeID("m-src", storage.NodeTypeEntityMention, 0),
		storage.ProvenanceSourceField: storage.MentionSourceRawContent,
	}
	mention := func(id, to string, metadata map[string]any) *storage.Edge {
		return &storage.Edge{
			ID: id, FromType: "object", FromID: "m-src", ToType: "entity", ToID: to,
			EdgeType: "mentions", Weight: 1, Metadata: metadata, CreatedAt: fixtureTime(),
		}
	}
	if err := es.Create(ctx, mention("m-prov", "person/alice", meta)); err != nil {
		t.Fatalf("Create with metadata: %v", err)
	}
	if err := es.Create(ctx, mention("m-bare", "org/acme", nil)); err != nil {
		t.Fatalf("Create without metadata: %v", err)
	}

	reads := []struct {
		name string
		list func() ([]*storage.Edge, error)
	}{
		{"ListFrom", func() ([]*storage.Edge, error) { return es.ListFrom(ctx, "object", "m-src") }},
		{"ListTo", func() ([]*storage.Edge, error) { return es.ListTo(ctx, "entity", "person/alice") }},
	}
	for _, r := range reads {
		got, err := r.list()
		if err != nil {
			t.Fatalf("%s: %v", r.name, err)
		}
		var prov *storage.Edge
		for _, e := range got {
			if e.ID == "m-prov" {
				prov = e
			}
		}
		if prov == nil {
			t.Fatalf("%s: m-prov missing from %d edges", r.name, len(got))
		}
		if !reflect.DeepEqual(prov.Metadata, meta) {
			t.Errorf("%s: m-prov metadata = %v, want %v", r.name, prov.Metadata, meta)
		}
	}

	got, err := es.ListTo(ctx, "entity", "org/acme")
	if err != nil || len(got) != 1 {
		t.Fatalf("ListTo org/acme = %v, %v; want the bare edge", got, err)
	}
	if len(got[0].Metadata) != 0 {
		t.Errorf("bare edge metadata = %v, want none", got[0].Metadata)
	}
}

// edgeIDs lists the IDs of every edge leaving the given objects, sorted.
func edgeIDs(t *testing.T, drv storage.StorageDriver, from ...string) []string {
	t.Helper()
	var ids []string
	for _, f := range from {
		edges, err := drv.Edges().ListFrom(context.Background(), "object", f)
		if err != nil {
			t.Fatalf("ListFrom %s: %v", f, err)
		}
		for _, e := range edges {
			ids = append(ids, e.ID)
		}
	}
	slices.Sort(ids)
	return ids
}

// EdgeBatchConformance pins EdgeStore.CreateAll and DeleteMatching, the
// writes that keep a typed link and its inverse together: CreateAll
// stores every edge or none, and DeleteMatching removes exactly the
// edges its matches select, in one transaction.
func EdgeBatchConformance(t *testing.T, drv storage.StorageDriver) {
	t.Helper()
	ctx := context.Background()
	es := drv.Edges()

	if err := es.CreateAll(ctx, nil); err != nil {
		t.Fatalf("CreateAll(nil): %v", err)
	}

	pair := []*storage.Edge{objectEdge("e-fwd", "a", "extends", "b"), objectEdge("e-rev", "b", "extended-by", "a")}
	if err := es.CreateAll(ctx, pair); err != nil {
		t.Fatalf("CreateAll: %v", err)
	}
	got, err := es.ListFrom(ctx, "object", "a")
	if err != nil || len(got) != 1 {
		t.Fatalf("ListFrom a = %v, %v; want the forward edge", got, err)
	}
	if e := got[0]; e.ID != "e-fwd" || e.ToID != "b" || e.EdgeType != "extends" || e.Metadata["context"] != "c-e-fwd" {
		t.Errorf("forward edge = %+v", e)
	}

	// A batch whose second edge cannot be stored (its ID is taken)
	// stores nothing: the first edge rolls back with it.
	err = es.CreateAll(ctx, []*storage.Edge{objectEdge("e-new", "c", "supports", "d"), objectEdge("e-fwd", "d", "supported-by", "c")})
	if err == nil {
		t.Fatal("CreateAll with a duplicate ID succeeded")
	}
	if ids := edgeIDs(t, drv, "c", "d"); len(ids) != 0 {
		t.Fatalf("a failed CreateAll left edges %v", ids)
	}

	// Edges a delete must leave alone: another type between the same
	// pair, the same type between another pair, and a mention edge.
	keep := []*storage.Edge{
		objectEdge("k-type", "a", "supports", "b"),
		objectEdge("k-pair", "a", "extends", "c"),
		{ID: "k-mention", FromType: "object", FromID: "a", ToType: "entity", ToID: "b", EdgeType: "extends", Weight: 1, CreatedAt: fixtureTime()},
	}
	if err := es.CreateAll(ctx, keep); err != nil {
		t.Fatalf("CreateAll keep: %v", err)
	}

	if _, err := es.DeleteMatching(ctx, []storage.EdgeMatch{{FromType: "object", FromID: "a", ToType: "object", ToID: "b"}}); err == nil {
		t.Error("DeleteMatching without edge types succeeded")
	}
	if ids := edgeIDs(t, drv, "a", "b"); !slices.Equal(ids, []string{"e-fwd", "e-rev", "k-mention", "k-pair", "k-type"}) {
		t.Fatalf("a refused DeleteMatching changed the edges: %v", ids)
	}

	n, err := es.DeleteMatching(ctx, []storage.EdgeMatch{
		{FromType: "object", FromID: "a", ToType: "object", ToID: "b", EdgeTypes: []string{"extends"}},
		{FromType: "object", FromID: "b", ToType: "object", ToID: "a", EdgeTypes: []string{"extended-by"}},
	})
	if err != nil || n != 2 {
		t.Fatalf("DeleteMatching = %d, %v; want 2", n, err)
	}
	if ids := edgeIDs(t, drv, "a", "b"); !slices.Equal(ids, []string{"k-mention", "k-pair", "k-type"}) {
		t.Errorf("after DeleteMatching: %v", ids)
	}

	n, err = es.DeleteMatching(ctx, []storage.EdgeMatch{
		{FromType: "object", FromID: "a", ToType: "object", ToID: "b", EdgeTypes: []string{"extends"}},
	})
	if err != nil || n != 0 {
		t.Errorf("DeleteMatching of nothing = %d, %v; want 0, nil", n, err)
	}
}

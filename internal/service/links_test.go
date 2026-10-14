package service

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func seedLinkObjects(t *testing.T, svc *Service, ids ...string) {
	t.Helper()
	now := time.Now().Truncate(time.Second)
	for _, id := range ids {
		if err := svc.Store.Objects().Create(context.Background(), &storage.KnowledgeObject{
			ID: id, Type: "text", CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatal(err)
		}
	}
}

// linkTriples lists every object-to-object edge leaving ids as
// "from type to", sorted.
func linkTriples(t *testing.T, svc *Service, ids ...string) []string {
	t.Helper()
	var out []string
	for _, id := range ids {
		edges, err := svc.Store.Edges().ListFrom(context.Background(), "object", id)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range edges {
			if e.ToType == "object" {
				out = append(out, e.FromID+" "+e.EdgeType+" "+e.ToID)
			}
		}
	}
	slices.Sort(out)
	return out
}

func TestCreateLinkStoresInverse(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	seedLinkObjects(t, svc, "a", "b")

	edges, err := svc.CreateLink(ctx, LinkRequest{Source: "a", Target: "b", Type: "extends", Context: "why"})
	if err != nil {
		t.Fatal(err)
	}
	if len(edges) != 2 || edges[0].EdgeType != "extends" || edges[1].EdgeType != "extended-by" {
		t.Fatalf("returned edges = %+v", edges)
	}
	for _, e := range edges {
		if e.Metadata["context"] != "why" || e.FromType != "object" || e.ToType != "object" || e.ID == "" {
			t.Errorf("edge = %+v", e)
		}
	}
	if got := linkTriples(t, svc, "a", "b"); !slices.Equal(got, []string{"a extends b", "b extended-by a"}) {
		t.Errorf("stored = %v", got)
	}

	// An inverse name stores its own inverse; a symmetric type one edge.
	if _, err := svc.CreateLink(ctx, LinkRequest{Source: "a", Target: "b", Type: "superseded-by"}); err != nil {
		t.Fatal(err)
	}
	edges, err = svc.CreateLink(ctx, LinkRequest{Source: "a", Target: "b", Type: "related-to"})
	if err != nil || len(edges) != 1 {
		t.Fatalf("symmetric: %+v %v", edges, err)
	}
	want := []string{"a extends b", "a related-to b", "a superseded-by b", "b extended-by a", "b supersedes a"}
	if got := linkTriples(t, svc, "a", "b"); !slices.Equal(got, want) {
		t.Errorf("stored = %v, want %v", got, want)
	}
}

func TestCreateLinkRefusals(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	seedLinkObjects(t, svc, "a", "b")

	for _, req := range []LinkRequest{
		{Source: "a", Target: "b", Type: "imaginary"},
		{Source: "a", Target: "b"},
		{Target: "b", Type: "extends"},
		{Source: "a", Type: "extends"},
	} {
		if _, err := svc.CreateLink(ctx, req); !errors.Is(err, ErrInvalidLink) {
			t.Errorf("%+v: err = %v, want ErrInvalidLink", req, err)
		}
	}
	for _, req := range []LinkRequest{
		{Source: "missing", Target: "b", Type: "extends"},
		{Source: "a", Target: "missing", Type: "extends"},
	} {
		if _, err := svc.CreateLink(ctx, req); !errors.Is(err, storage.ErrNotFound) {
			t.Errorf("%+v: err = %v, want ErrNotFound", req, err)
		}
	}
	if got := linkTriples(t, svc, "a", "b"); len(got) != 0 {
		t.Errorf("a refused link stored %v", got)
	}
}

// A link and its inverse go in together: when the pair cannot be stored,
// neither edge is.
func TestCreateLinkPairIsAtomic(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	seedLinkObjects(t, svc, "a", "b")

	svc.Store = failingPairStore{StorageDriver: svc.Store}
	if _, err := svc.CreateLink(ctx, LinkRequest{Source: "a", Target: "b", Type: "extends"}); err == nil {
		t.Fatal("CreateLink succeeded over a failing store")
	}
	if got := linkTriples(t, svc, "a", "b"); len(got) != 0 {
		t.Errorf("a failed link left %v", got)
	}
}

// failingPairStore stores a batch's first edge, then fails on the second:
// the partial write an unbatched pair would leave behind.
type failingPairStore struct{ storage.StorageDriver }

func (f failingPairStore) Edges() storage.EdgeStore {
	return failingPairEdges{EdgeStore: f.StorageDriver.Edges()}
}

type failingPairEdges struct{ storage.EdgeStore }

var errPairFailed = errors.New("second edge refused")

func (f failingPairEdges) Create(ctx context.Context, e *storage.Edge) error {
	if e.EdgeType == "extended-by" {
		return errPairFailed
	}
	return f.EdgeStore.Create(ctx, e)
}

func (f failingPairEdges) CreateAll(ctx context.Context, edges []*storage.Edge) error {
	for _, e := range edges {
		if e.EdgeType == "extended-by" {
			return errPairFailed
		}
	}
	return f.EdgeStore.CreateAll(ctx, edges)
}

func TestDeleteLinksRemovesBothDirections(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	seedLinkObjects(t, svc, "a", "b", "c")
	for _, r := range []LinkRequest{
		{Source: "a", Target: "b", Type: "extends"},
		{Source: "b", Target: "a", Type: "supports"},
		{Source: "a", Target: "b", Type: "related-to"},
		{Source: "a", Target: "c", Type: "extends"},
	} {
		if _, err := svc.CreateLink(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	// A non-link object edge between the pair is not a link.
	if err := svc.Store.Edges().Create(ctx, &storage.Edge{
		ID: "alt", FromType: "object", FromID: "a", ToType: "object", ToID: "b", EdgeType: "alternative", CreatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	// With a type: that link and its inverse only.
	n, err := svc.DeleteLinks(ctx, "b", "a", "extended-by")
	if err != nil || n != 2 {
		t.Fatalf("typed delete = %d, %v; want 2", n, err)
	}
	want := []string{"a alternative b", "a extends c", "a related-to b", "a supported-by b", "b supports a", "c extended-by a"}
	if got := linkTriples(t, svc, "a", "b", "c"); !slices.Equal(got, want) {
		t.Fatalf("after typed delete = %v, want %v", got, want)
	}

	// Without: every link either way between the pair.
	n, err = svc.DeleteLinks(ctx, "a", "b", "")
	if err != nil || n != 3 {
		t.Fatalf("delete all = %d, %v; want 3", n, err)
	}
	want = []string{"a alternative b", "a extends c", "c extended-by a"}
	if got := linkTriples(t, svc, "a", "b", "c"); !slices.Equal(got, want) {
		t.Errorf("after delete = %v, want %v", got, want)
	}

	if n, err := svc.DeleteLinks(ctx, "a", "b", ""); err != nil || n != 0 {
		t.Errorf("nothing left: %d, %v", n, err)
	}
	for _, c := range [][3]string{{"a", "b", "imaginary"}, {"", "b", ""}, {"a", "", ""}} {
		if _, err := svc.DeleteLinks(ctx, c[0], c[1], c[2]); !errors.Is(err, ErrInvalidLink) {
			t.Errorf("%v: err = %v, want ErrInvalidLink", c, err)
		}
	}
}

func hopStrings(hops []LinkHop) []string {
	out := make([]string, 0, len(hops))
	for _, h := range hops {
		out = append(out, string(rune('0'+h.Depth))+" "+h.From+" "+h.Direction+" "+h.Edge.EdgeType+" "+h.ObjectID)
	}
	return out
}

func TestListLinks(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	seedLinkObjects(t, svc, "a", "b", "c", "d", "e")
	for _, r := range []LinkRequest{
		{Source: "a", Target: "b", Type: "extends"},
		{Source: "b", Target: "c", Type: "supports"},
		{Source: "c", Target: "d", Type: "related-to"},
		{Source: "d", Target: "e", Type: "contradicts"},
	} {
		if _, err := svc.CreateLink(ctx, r); err != nil {
			t.Fatal(err)
		}
	}

	cases := []struct {
		name string
		q    LinkQuery
		want []string
	}{
		{"every edge", LinkQuery{ObjectID: "b"}, []string{
			"1 b out extended-by a", "1 b out supports c", "1 b in extends a", "1 b in supported-by c",
		}},
		{"out only", LinkQuery{ObjectID: "b", Direction: "out"}, []string{"1 b out extended-by a", "1 b out supports c"}},
		{"in only", LinkQuery{ObjectID: "b", Direction: "in"}, []string{"1 b in extends a", "1 b in supported-by c"}},
		{"type", LinkQuery{ObjectID: "b", Type: "supports"}, []string{"1 b out supports c"}},
		{"depth 3", LinkQuery{ObjectID: "a", Depth: 3}, []string{
			"1 a out extends b", "2 b out supports c", "3 c out related-to d",
		}},
		{"depth 1 visits each object once", LinkQuery{ObjectID: "b", Depth: 1}, []string{
			"1 b out extended-by a", "1 b out supports c",
		}},
		{"depth 3 inbound", LinkQuery{ObjectID: "e", Depth: 3, Direction: "in"}, []string{
			"1 e in contradicts d", "2 d in related-to c", "3 c in supports b",
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hops, err := svc.ListLinks(ctx, tc.q)
			if err != nil {
				t.Fatal(err)
			}
			if got := hopStrings(hops); !slices.Equal(got, tc.want) {
				t.Errorf("hops = %v\nwant %v", got, tc.want)
			}
		})
	}

	if _, err := svc.ListLinks(ctx, LinkQuery{ObjectID: "missing"}); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("missing object: %v", err)
	}
	for _, q := range []LinkQuery{{ObjectID: "a", Direction: "sideways"}, {ObjectID: "a", Depth: 4}, {ObjectID: "a", Depth: -1}} {
		if _, err := svc.ListLinks(ctx, q); !errors.Is(err, ErrInvalidLink) {
			t.Errorf("%+v: err = %v, want ErrInvalidLink", q, err)
		}
	}
}

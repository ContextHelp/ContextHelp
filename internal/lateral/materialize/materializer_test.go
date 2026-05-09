package materialize

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/lateral/identity"
)

type fakeStore struct {
	written []map[string]any
	edges   []Edge
}

func (s *fakeStore) WriteRecord(_ context.Context, rec map[string]any) (string, error) {
	s.written = append(s.written, rec)
	return "o-lc-" + rec["title"].(string), nil
}

func (s *fakeStore) WriteEdge(_ context.Context, e Edge) error {
	s.edges = append(s.edges, e)
	return nil
}

func TestMaterialize_EdgeOnlyWhenCanonicalExists(t *testing.T) {
	st := &fakeStore{}
	m := New(st)
	in := Input{
		ParentID:      "o-parent",
		URL:           "https://x.com/y",
		Title:         "y",
		CandidateType: "sibling_repo",
		Strategy:      "github.owner",
		Resolution:    identity.Result{EdgeOnly: true, CanonicalID: "o-canonical"},
	}
	out, err := m.Materialize(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if !out.EdgeOnly {
		t.Fatal("expected edge-only outcome")
	}
	if len(st.written) != 0 {
		t.Fatal("expected no probationary record")
	}
	if len(st.edges) != 1 || st.edges[0].Type != "refers_to_canonical" {
		t.Fatalf("expected refers_to_canonical edge, got %+v", st.edges)
	}
}

func TestMaterialize_ProbationaryWhenNoCanonical(t *testing.T) {
	st := &fakeStore{}
	m := New(st)
	in := Input{
		ParentID:      "o-parent",
		URL:           "https://x.com/y",
		Title:         "y",
		CandidateType: "sibling_repo",
		Strategy:      "github.owner",
		Resolution:    identity.Result{EdgeOnly: false},
	}
	out, err := m.Materialize(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if out.EdgeOnly {
		t.Fatal("expected probationary, got edge-only")
	}
	if len(st.written) != 1 {
		t.Fatalf("expected 1 probationary record, got %d", len(st.written))
	}
	if len(st.edges) != 1 || st.edges[0].Type != "discovered_by" {
		t.Fatalf("expected discovered_by edge, got %+v", st.edges)
	}
}

package graph_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/graph"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// -------------------------------------------------------------------------
// Helpers / stubs
// -------------------------------------------------------------------------

// stubEntityStore is a minimal EntityStore for testing.
type stubEntityStore struct {
	entities map[string]*storage.Entity
}

func newStubStore(slugs ...string) *stubEntityStore {
	m := map[string]*storage.Entity{}
	for _, s := range slugs {
		m[s] = &storage.Entity{
			Slug:      s,
			Title:     s,
			Namespace: firstPart(s),
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
	}
	return &stubEntityStore{entities: m}
}

func firstPart(slug string) string {
	for i, c := range slug {
		if c == '.' {
			return slug[:i]
		}
	}
	return slug
}

func (s *stubEntityStore) Upsert(_ context.Context, e *storage.Entity) error {
	s.entities[e.Slug] = e
	return nil
}
func (s *stubEntityStore) UpsertThin(_ context.Context, e *storage.Entity) error {
	s.entities[e.Slug] = e
	return nil
}
func (s *stubEntityStore) SetContentStatus(_ context.Context, slug string, status storage.ContentStatus) error {
	if e, ok := s.entities[slug]; ok {
		e.ContentStatus = status
		return nil
	}
	return errors.New("not found")
}
func (s *stubEntityStore) Get(_ context.Context, slug string) (*storage.Entity, error) {
	if e, ok := s.entities[slug]; ok {
		return e, nil
	}
	return nil, errors.New("not found")
}
func (s *stubEntityStore) List(_ context.Context, _ storage.EntityFilter) ([]*storage.Entity, error) {
	var out []*storage.Entity
	for _, e := range s.entities {
		out = append(out, e)
	}
	return out, nil
}
func (s *stubEntityStore) Resolve(_ context.Context, mention string) (*storage.Entity, error) {
	if e, ok := s.entities[mention]; ok {
		return e, nil
	}
	return nil, errors.New("not found: " + mention)
}

// stubEdgeStore tracks edges in memory.
type stubEdgeStore struct {
	edges []*storage.Edge
}

func (s *stubEdgeStore) Create(_ context.Context, e *storage.Edge) error {
	s.edges = append(s.edges, e)
	return nil
}
func (s *stubEdgeStore) ListFrom(_ context.Context, fromType, fromID string) ([]*storage.Edge, error) {
	var out []*storage.Edge
	for _, e := range s.edges {
		if e.FromType == fromType && e.FromID == fromID {
			out = append(out, e)
		}
	}
	return out, nil
}
func (s *stubEdgeStore) ListTo(_ context.Context, toType, toID string) ([]*storage.Edge, error) {
	var out []*storage.Edge
	for _, e := range s.edges {
		if e.ToType == toType && e.ToID == toID {
			out = append(out, e)
		}
	}
	return out, nil
}
func (s *stubEdgeStore) Delete(_ context.Context, id string) error {
	for i, e := range s.edges {
		if e.ID == id {
			s.edges = append(s.edges[:i], s.edges[i+1:]...)
			return nil
		}
	}
	return errors.New("not found")
}
func (s *stubEdgeStore) DeleteByObject(_ context.Context, objectID string) error {
	var kept []*storage.Edge
	for _, e := range s.edges {
		if e.FromID != objectID && e.ToID != objectID {
			kept = append(kept, e)
		}
	}
	s.edges = kept
	return nil
}
func (s *stubEdgeStore) RelatedObjectIDs(_ context.Context, objectID string, depth, limit int) ([]string, error) {
	return nil, nil
}
func (s *stubEdgeStore) CountMentionsTo(_ context.Context, toType, toID string) (int, error) {
	n := 0
	for _, e := range s.edges {
		if e.ToType == toType && e.ToID == toID {
			n++
		}
	}
	return n, nil
}

// -------------------------------------------------------------------------
// EntityIntegrityGuard tests
// -------------------------------------------------------------------------

func TestEntityIntegrityGuard_RequiredFields(t *testing.T) {
	g := graph.NewEntityIntegrityGuard()
	ctx := context.Background()

	cases := []struct {
		name    string
		entity  *storage.Entity
		wantErr bool
	}{
		{
			name: "valid entity",
			entity: &storage.Entity{
				Slug: "stripe.checkout", Title: "Checkout", Namespace: "stripe",
			},
			wantErr: false,
		},
		{
			name:    "missing slug",
			entity:  &storage.Entity{Title: "no-slug", Namespace: "stripe"},
			wantErr: true,
		},
		{
			name:    "missing title",
			entity:  &storage.Entity{Slug: "stripe.checkout", Namespace: "stripe"},
			wantErr: true,
		},
		{
			name:    "missing namespace",
			entity:  &storage.Entity{Slug: "stripe.checkout", Title: "Checkout"},
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			vs := g.Validate(ctx, tc.entity, "test")
			if tc.wantErr && len(vs) == 0 {
				t.Errorf("expected violation, got none")
			}
			if !tc.wantErr && len(vs) > 0 {
				t.Errorf("unexpected violation: %s", vs.Error())
			}
		})
	}
}

func TestEntityIntegrityGuard_SlugFormat(t *testing.T) {
	g := graph.NewEntityIntegrityGuard()
	ctx := context.Background()

	cases := []struct {
		name    string
		entity  *storage.Entity
		wantErr bool
		rule    string
	}{
		{
			name:    "uppercase slug rejected",
			entity:  &storage.Entity{Slug: "Stripe.Checkout", Title: "T", Namespace: "stripe"},
			wantErr: true,
			rule:    "slug_format",
		},
		{
			name:    "uppercase namespace rejected",
			entity:  &storage.Entity{Slug: "stripe.checkout", Title: "T", Namespace: "Stripe"},
			wantErr: true,
			rule:    "namespace_format",
		},
		{
			name:    "invalid chars in slug rejected",
			entity:  &storage.Entity{Slug: "stripe.check_out", Title: "T", Namespace: "stripe"},
			wantErr: true,
			rule:    "slug_format",
		},
		{
			name:    "valid hyphen slug accepted",
			entity:  &storage.Entity{Slug: "stripe.check-out", Title: "T", Namespace: "stripe"},
			wantErr: false,
		},
		{
			name:    "multi-segment slug accepted",
			entity:  &storage.Entity{Slug: "stripe.api.checkout", Title: "T", Namespace: "stripe"},
			wantErr: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			vs := g.Validate(ctx, tc.entity, "test")
			if tc.wantErr && len(vs) == 0 {
				t.Errorf("expected violation, got none")
			}
			if !tc.wantErr && len(vs) > 0 {
				t.Errorf("unexpected violation: %s", vs.Error())
			}
			if tc.wantErr && len(vs) > 0 && tc.rule != "" {
				found := false
				for _, v := range vs {
					if v.Rule == tc.rule {
						found = true
					}
				}
				if !found {
					t.Errorf("expected rule %q, got violations: %s", tc.rule, vs.Error())
				}
			}
		})
	}
}

func TestEntityIntegrityGuard_NamespaceOwnership(t *testing.T) {
	g := graph.NewEntityIntegrityGuard()
	g.LoadNamespaces(map[string]string{
		"stripe": "https://registry.example.com/stripe",
	})
	ctx := context.Background()

	t.Run("known namespace accepted", func(t *testing.T) {
		vs := g.Validate(ctx, &storage.Entity{
			Slug: "stripe.checkout", Title: "Checkout", Namespace: "stripe",
		}, "test")
		if len(vs) > 0 {
			t.Errorf("unexpected violation: %s", vs.Error())
		}
	})

	t.Run("unknown namespace rejected", func(t *testing.T) {
		vs := g.Validate(ctx, &storage.Entity{
			Slug: "evil.checkout", Title: "Checkout", Namespace: "evil",
		}, "test")
		if len(vs) == 0 {
			t.Errorf("expected namespace_ownership violation, got none")
		}
	})

	t.Run("no registry loaded accepts any namespace", func(t *testing.T) {
		g2 := graph.NewEntityIntegrityGuard()
		vs := g2.Validate(ctx, &storage.Entity{
			Slug: "anyns.slug", Title: "T", Namespace: "anyns",
		}, "test")
		if len(vs) > 0 {
			t.Errorf("unexpected violation when no registry loaded: %s", vs.Error())
		}
	})
}

// -------------------------------------------------------------------------
// GraphSafetyValidator tests
// -------------------------------------------------------------------------

func TestGraphSafetyValidator_ValidateMention(t *testing.T) {
	store := newStubStore("stripe.checkout", "project.dashboard")
	v := graph.NewGraphSafetyValidator(store)
	ctx := context.Background()

	cases := []struct {
		mention string
		wantErr bool
	}{
		{"@stripe.checkout", false},
		{"ctxt://entity/stripe/checkout", false},
		{"@project.dashboard", false},
		// syntax errors
		{"@BAD.Slug", true},
		{"@stripe", true}, // single part — not a valid mention
		{"ctxt://entity/", true},
		// exists check
		{"@stripe.nonexistent", true},
		{"ctxt://entity/stripe/nonexistent", true},
	}

	for _, tc := range cases {
		t.Run(tc.mention, func(t *testing.T) {
			err := v.ValidateMention(ctx, tc.mention, "test")
			if tc.wantErr && err == nil {
				t.Errorf("expected error for %q, got nil", tc.mention)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("unexpected error for %q: %v", tc.mention, err)
			}
		})
	}
}

func TestGraphSafetyValidator_ValidateEdge(t *testing.T) {
	store := newStubStore("stripe.checkout")
	v := graph.NewGraphSafetyValidator(store)
	ctx := context.Background()

	t.Run("valid entity edge accepted", func(t *testing.T) {
		edge := &storage.Edge{
			ID: "e1", FromType: "object", FromID: "obj1",
			ToType: "entity", ToID: "stripe.checkout",
			EdgeType: "mention",
		}
		vs := v.ValidateEdge(ctx, edge, "test")
		if len(vs) > 0 {
			t.Errorf("unexpected violation: %s", vs.Error())
		}
	})

	t.Run("missing entity rejected", func(t *testing.T) {
		edge := &storage.Edge{
			ID: "e2", FromType: "object", FromID: "obj1",
			ToType: "entity", ToID: "stripe.ghost",
			EdgeType: "mention",
		}
		vs := v.ValidateEdge(ctx, edge, "test")
		if len(vs) == 0 {
			t.Errorf("expected edge_target_missing violation")
		}
	})

	t.Run("malformed entity ID rejected", func(t *testing.T) {
		edge := &storage.Edge{
			ID: "e3", FromType: "object", FromID: "obj1",
			ToType: "entity", ToID: "@INVALID",
			EdgeType: "mention",
		}
		vs := v.ValidateEdge(ctx, edge, "test")
		if len(vs) == 0 {
			t.Errorf("expected edge_target_syntax violation")
		}
	})

	t.Run("non-entity edge skipped", func(t *testing.T) {
		edge := &storage.Edge{
			ID: "e4", FromType: "object", FromID: "obj1",
			ToType: "object", ToID: "some-obj-id",
			EdgeType: "related",
		}
		vs := v.ValidateEdge(ctx, edge, "test")
		if len(vs) > 0 {
			t.Errorf("non-entity edge should not be validated: %s", vs.Error())
		}
	})
}

// -------------------------------------------------------------------------
// DetectCycles
// -------------------------------------------------------------------------

func TestDetectCycles_NoCycle(t *testing.T) {
	// a → b → c (linear, no cycle)
	graph_map := map[string][]string{
		"a": {"b"},
		"b": {"c"},
		"c": {},
	}
	err := graph.DetectCycles(context.Background(), "a",
		func(_ context.Context, id string) ([]string, error) {
			return graph_map[id], nil
		})
	if err != nil {
		t.Errorf("expected no cycle, got: %v", err)
	}
}

func TestDetectCycles_WithCycle(t *testing.T) {
	// a → b → c → a
	graph_map := map[string][]string{
		"a": {"b"},
		"b": {"c"},
		"c": {"a"},
	}
	err := graph.DetectCycles(context.Background(), "a",
		func(_ context.Context, id string) ([]string, error) {
			return graph_map[id], nil
		})
	if err == nil {
		t.Error("expected cycle error, got nil")
	}
}

func TestDetectCycles_SelfLoop(t *testing.T) {
	graph_map := map[string][]string{"a": {"a"}}
	err := graph.DetectCycles(context.Background(), "a",
		func(_ context.Context, id string) ([]string, error) {
			return graph_map[id], nil
		})
	if err == nil {
		t.Error("expected self-loop cycle error, got nil")
	}
}

// -------------------------------------------------------------------------
// FindOrphanedEdges
// -------------------------------------------------------------------------

func TestFindOrphanedEdges(t *testing.T) {
	es := newStubStore("stripe.checkout")
	edgeStore := &stubEdgeStore{
		edges: []*storage.Edge{
			{ID: "e1", FromType: "object", FromID: "obj1", ToType: "entity", ToID: "stripe.checkout"},
			{ID: "e2", FromType: "object", FromID: "obj1", ToType: "entity", ToID: "stripe.ghost"},
		},
	}

	orphans, err := graph.FindOrphanedEdges(context.Background(), es, edgeStore, "object", "obj1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(orphans) != 1 || orphans[0] != "e2" {
		t.Errorf("expected orphan [e2], got %v", orphans)
	}
}

// -------------------------------------------------------------------------
// FindDuplicateMentions
// -------------------------------------------------------------------------

func TestFindDuplicateMentions(t *testing.T) {
	mentions := []string{
		"@stripe.checkout",
		"ctxt://entity/stripe/checkout", // same entity, different form
		"@project.dashboard",
	}
	dups := graph.FindDuplicateMentions(mentions)
	if len(dups) != 1 {
		t.Errorf("expected 1 duplicate (stripe.checkout), got %v", dups)
	}
}

func TestFindDuplicateMentions_NoDups(t *testing.T) {
	mentions := []string{"@stripe.checkout", "@project.dashboard"}
	dups := graph.FindDuplicateMentions(mentions)
	if len(dups) != 0 {
		t.Errorf("expected no duplicates, got %v", dups)
	}
}

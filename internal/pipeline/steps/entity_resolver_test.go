package steps

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	uri "hop.top/cite/scheme"
)

// ─── stubs ───────────────────────────────────────────────────────────────────

type stubEntityStore struct {
	entities map[string]*storage.Entity
	upserted []*storage.Entity
}

func newStubEntityStore(existing ...*storage.Entity) *stubEntityStore {
	s := &stubEntityStore{entities: make(map[string]*storage.Entity)}
	for _, e := range existing {
		s.entities[e.Slug] = e
	}
	return s
}

func (s *stubEntityStore) Upsert(_ context.Context, e *storage.Entity) error {
	s.entities[e.Slug] = e
	s.upserted = append(s.upserted, e)
	return nil
}

func (s *stubEntityStore) UpsertThin(_ context.Context, e *storage.Entity) error {
	if existing, ok := s.entities[e.Slug]; ok && existing.ContentStatus == storage.ContentStatusFull {
		return nil // don't overwrite full
	}
	s.entities[e.Slug] = e
	s.upserted = append(s.upserted, e)
	return nil
}

func (s *stubEntityStore) SetContentStatus(_ context.Context, slug string, status storage.ContentStatus) error {
	if e, ok := s.entities[slug]; ok {
		e.ContentStatus = status
	}
	return nil
}

func (s *stubEntityStore) Get(_ context.Context, slug string) (*storage.Entity, error) {
	if e, ok := s.entities[slug]; ok {
		return e, nil
	}
	return nil, fmt.Errorf("not found")
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
	return nil, fmt.Errorf("entity not found: %s", mention)
}

type stubEdgeStore struct {
	created []*storage.Edge
}

func (s *stubEdgeStore) Create(_ context.Context, e *storage.Edge) error {
	s.created = append(s.created, e)
	return nil
}

func (s *stubEdgeStore) ListFrom(_ context.Context, _, _ string) ([]*storage.Edge, error) {
	return nil, nil
}

func (s *stubEdgeStore) ListTo(_ context.Context, _, _ string) ([]*storage.Edge, error) {
	return nil, nil
}

func (s *stubEdgeStore) Delete(_ context.Context, _ string) error { return nil }

func (s *stubEdgeStore) DeleteByObject(_ context.Context, _ string) error { return nil }

func (s *stubEdgeStore) CountMentionsTo(_ context.Context, _, _ string) (int, error) {
	return 0, nil
}

func (s *stubEdgeStore) RelatedObjectIDs(_ context.Context, _ string, _, _ int) ([]string, error) {
	return nil, nil
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func mustURI(t *testing.T, s string) uri.URI {
	t.Helper()
	u, err := uri.Parse(s)
	if err != nil {
		t.Fatalf("parse URI %q: %v", s, err)
	}
	return *u
}

// ─── tests ───────────────────────────────────────────────────────────────────

func TestEntityResolverNilStores(t *testing.T) {
	step := NewEntityResolver()
	draft := &storage.KnowledgeObject{
		ID:       "obj-1",
		Mentions: []uri.URI{mustURI(t, "ctxt://entity/project/alpha")},
	}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// No-op: metadata unchanged, no edges.
	if len(got.Metadata) != 0 {
		t.Errorf("expected nil/empty metadata, got %v", got.Metadata)
	}
}

func TestEntityResolverNoMentions(t *testing.T) {
	entities := newStubEntityStore()
	edges := &stubEdgeStore{}
	step := NewEntityResolverWithStores(entities, edges)
	draft := &storage.KnowledgeObject{ID: "obj-1"}

	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(edges.created) != 0 {
		t.Errorf("expected 0 edges, got %d", len(edges.created))
	}
	_ = got
}

func TestEntityResolverResolvesKnownEntity(t *testing.T) {
	known := &storage.Entity{
		Slug:          "project/alpha",
		Title:         "Alpha",
		Namespace:     "project",
		ContentStatus: storage.ContentStatusFull,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}
	entities := newStubEntityStore(known)
	edges := &stubEdgeStore{}
	step := NewEntityResolverWithStores(entities, edges)

	draft := &storage.KnowledgeObject{
		ID:       "obj-1",
		Mentions: []uri.URI{mustURI(t, "ctxt://entity/project/alpha")},
	}

	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Resolved, not placeholder.
	if r, ok := got.Metadata["resolved_entities"].([]string); !ok || len(r) != 1 || r[0] != "project/alpha" {
		t.Errorf("expected resolved_entities=[project/alpha], got %v", got.Metadata)
	}
	if _, ok := got.Metadata["placeholder_entities"]; ok {
		t.Errorf("unexpected placeholder_entities in metadata")
	}

	// Edge created.
	if len(edges.created) != 1 {
		t.Fatalf("expected 1 edge, got %d", len(edges.created))
	}
	e := edges.created[0]
	if e.FromType != "object" || e.FromID != "obj-1" {
		t.Errorf("edge from: want object/obj-1, got %s/%s", e.FromType, e.FromID)
	}
	if e.ToType != "entity" || e.ToID != "project/alpha" {
		t.Errorf("edge to: want entity/project/alpha, got %s/%s", e.ToType, e.ToID)
	}
	if e.EdgeType != "mentions" {
		t.Errorf("edge type: want mentions, got %s", e.EdgeType)
	}
}

func TestEntityResolverCreatesPlaceholderForUnknown(t *testing.T) {
	entities := newStubEntityStore() // empty
	edges := &stubEdgeStore{}
	step := NewEntityResolverWithStores(entities, edges)

	draft := &storage.KnowledgeObject{
		ID:       "obj-2",
		Mentions: []uri.URI{mustURI(t, "ctxt://entity/person/jane-doe")},
	}

	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Placeholder created.
	if p, ok := got.Metadata["placeholder_entities"].([]string); !ok || len(p) != 1 || p[0] != "person/jane-doe" {
		t.Errorf("expected placeholder_entities=[person/jane-doe], got %v", got.Metadata)
	}

	// Entity upserted as thin.
	if len(entities.upserted) != 1 {
		t.Fatalf("expected 1 upserted entity, got %d", len(entities.upserted))
	}
	ent := entities.upserted[0]
	if ent.Slug != "person/jane-doe" {
		t.Errorf("entity slug: want person/jane-doe, got %s", ent.Slug)
	}
	if ent.ContentStatus != storage.ContentStatusThin {
		t.Errorf("entity status: want thin, got %s", ent.ContentStatus)
	}
	if ent.Namespace != "person" {
		t.Errorf("entity namespace: want person, got %s", ent.Namespace)
	}

	// Edge still created pointing to placeholder.
	if len(edges.created) != 1 {
		t.Fatalf("expected 1 edge, got %d", len(edges.created))
	}
}

func TestEntityResolverMixedMentions(t *testing.T) {
	known := &storage.Entity{
		Slug:          "org/acme",
		Title:         "Acme Corp",
		Namespace:     "org",
		ContentStatus: storage.ContentStatusFull,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}
	entities := newStubEntityStore(known)
	edges := &stubEdgeStore{}
	step := NewEntityResolverWithStores(entities, edges)

	draft := &storage.KnowledgeObject{
		ID: "obj-3",
		Mentions: []uri.URI{
			mustURI(t, "ctxt://entity/org/acme"),          // known
			mustURI(t, "ctxt://entity/project/new-thing"), // unknown → placeholder
		},
	}

	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if r, ok := got.Metadata["resolved_entities"].([]string); !ok || len(r) != 1 {
		t.Errorf("expected 1 resolved entity, got %v", got.Metadata["resolved_entities"])
	}
	if p, ok := got.Metadata["placeholder_entities"].([]string); !ok || len(p) != 1 {
		t.Errorf("expected 1 placeholder entity, got %v", got.Metadata["placeholder_entities"])
	}
	if len(edges.created) != 2 {
		t.Errorf("expected 2 edges, got %d", len(edges.created))
	}
}

func TestEntityResolverNoEdgeWithoutObjectID(t *testing.T) {
	known := &storage.Entity{
		Slug:          "concept/design",
		Title:         "Design",
		Namespace:     "concept",
		ContentStatus: storage.ContentStatusFull,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}
	entities := newStubEntityStore(known)
	edges := &stubEdgeStore{}
	step := NewEntityResolverWithStores(entities, edges)

	// No ID set → no edge.
	draft := &storage.KnowledgeObject{
		Mentions: []uri.URI{mustURI(t, "ctxt://entity/concept/design")},
	}

	_, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(edges.created) != 0 {
		t.Errorf("expected 0 edges when object ID is empty, got %d", len(edges.created))
	}
}

func TestEntityResolverDoesNotWriteIntraObjectGraph(t *testing.T) {
	// Boundary test (ADR-063): entity_resolver writes storage.Edge (inter-object) only.
	// It must NOT touch ko.Graph — that layer belongs to entity_extractor.
	known := &storage.Entity{
		Slug:          "project/beta",
		Title:         "Beta",
		Namespace:     "project",
		ContentStatus: storage.ContentStatusFull,
	}
	entities := newStubEntityStore(known)
	edges := &stubEdgeStore{}
	step := NewEntityResolverWithStores(entities, edges)

	draft := &storage.KnowledgeObject{
		ID:       "obj-er1",
		Mentions: []uri.URI{mustURI(t, "ctxt://entity/project/beta")},
	}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	// Inter-object edge written.
	if len(edges.created) != 1 {
		t.Errorf("expected 1 inter-object edge, got %d", len(edges.created))
	}
	// Intra-object graph must remain untouched.
	if got.Graph != nil {
		t.Error("entity_resolver must not write to ko.Graph (intra-object layer)")
	}
}

func TestMentionSlug(t *testing.T) {
	cases := []struct {
		uriStr string
		want   string
	}{
		{"ctxt://entity/project/alpha", "project/alpha"},
		{"ctxt://entity/person/alice", "person/alice"},
		{"ctxt://entity/stripe/api/checkout", "stripe/api/checkout"},
		{"ctxt://object/foo", ""},
		{"other://entity/bar", ""},
	}
	for _, tc := range cases {
		u, err := uri.Parse(tc.uriStr)
		if err != nil {
			t.Fatalf("parse %q: %v", tc.uriStr, err)
		}
		got := mentionSlug(u)
		if got != tc.want {
			t.Errorf("mentionSlug(%q) = %q, want %q", tc.uriStr, got, tc.want)
		}
	}
}

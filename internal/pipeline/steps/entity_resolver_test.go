package steps

import (
	"context"
	"fmt"
	"reflect"
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

func (s *stubEntityStore) Namespaces(_ context.Context) ([]string, error) { return nil, nil }

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

func (s *stubEdgeStore) CreateAll(ctx context.Context, edges []*storage.Edge) error {
	for _, e := range edges {
		_ = s.Create(ctx, e)
	}
	return nil
}

// CreateMissing keeps the store's contract: an edge between the same
// nodes with the same type is stored once.
func (s *stubEdgeStore) CreateMissing(ctx context.Context, edges []*storage.Edge) (int, error) {
	n := 0
	for _, e := range edges {
		if s.has(e) {
			continue
		}
		_ = s.Create(ctx, e)
		n++
	}
	return n, nil
}

func (s *stubEdgeStore) has(e *storage.Edge) bool {
	for _, c := range s.created {
		if c.FromType == e.FromType && c.FromID == e.FromID && c.ToType == e.ToType && c.ToID == e.ToID && c.EdgeType == e.EdgeType {
			return true
		}
	}
	return false
}

func (s *stubEdgeStore) DeleteMatching(_ context.Context, _ []storage.EdgeMatch) (int, error) {
	return 0, nil
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
	// The edge names the entity by the mention's canonical URI, as every
	// mention-edge writer does, never by the bare slug.
	if e.ToType != "entity" || e.ToID != "ctxt://entity/project/alpha" {
		t.Errorf("edge to: want entity/ctxt://entity/project/alpha, got %s/%s", e.ToType, e.ToID)
	}
	if e.EdgeType != "mentions" {
		t.Errorf("edge type: want mentions, got %s", e.EdgeType)
	}
}

// TestEntityResolverOneEdgePerEntity pins that a mention repeated on the
// draft, and a second run over the same draft (a retried job), add no
// second edge to the entity.
func TestEntityResolverOneEdgePerEntity(t *testing.T) {
	entities := newStubEntityStore()
	edges := &stubEdgeStore{}
	step := NewEntityResolverWithStores(entities, edges)

	draft := &storage.KnowledgeObject{
		ID: "obj-dup",
		Mentions: []uri.URI{
			mustURI(t, "ctxt://entity/project/alpha"),
			mustURI(t, "ctxt://entity/project/alpha"),
		},
	}
	for run := 1; run <= 2; run++ {
		if _, err := step.Run(context.Background(), draft); err != nil {
			t.Fatalf("run %d: %v", run, err)
		}
		if len(edges.created) != 1 {
			t.Fatalf("run %d: expected 1 edge, got %d", run, len(edges.created))
		}
	}
	if e := edges.created[0]; e.ToID != "ctxt://entity/project/alpha" || e.EdgeType != "mentions" {
		t.Errorf("edge = %s %s, want a mentions edge to ctxt://entity/project/alpha", e.EdgeType, e.ToID)
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

// TestEntityResolverRecordsProvenance pins the provenance each mention
// edge carries: the entity_mention node of the draft's graph that holds
// the mention and the field that node recorded, or the mentions field
// alone when the graph has no node for it.
func TestEntityResolverRecordsProvenance(t *testing.T) {
	entities := newStubEntityStore()
	edges := &stubEdgeStore{}
	step := NewEntityResolverWithStores(entities, edges)

	nodeID := storage.NewNodeID("obj-3", storage.NodeTypeEntityMention, 0)
	draft := &storage.KnowledgeObject{
		ID: "obj-3",
		Mentions: []uri.URI{
			mustURI(t, "ctxt://entity/person/alice"),
			mustURI(t, "ctxt://entity/org/acme"),
		},
		Graph: &storage.ObjectGraph{Nodes: []storage.GraphNode{{
			ID:       nodeID,
			NodeType: storage.NodeTypeEntityMention,
			Label:    "ctxt://entity/person/alice",
			Metadata: map[string]any{storage.ProvenanceSourceField: storage.MentionSourceRawContent},
		}}},
	}
	if _, err := step.Run(context.Background(), draft); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []map[string]any{
		{storage.ProvenanceSourceNode: nodeID, storage.ProvenanceSourceField: storage.MentionSourceRawContent},
		{storage.ProvenanceSourceNode: "", storage.ProvenanceSourceField: storage.MentionSourceMentions},
	}
	if len(edges.created) != len(want) {
		t.Fatalf("expected %d edges, got %d", len(want), len(edges.created))
	}
	for i, e := range edges.created {
		if !reflect.DeepEqual(e.Metadata, want[i]) {
			t.Errorf("edge[%d] %s metadata = %v, want %v", i, e.ToID, e.Metadata, want[i])
		}
	}
}

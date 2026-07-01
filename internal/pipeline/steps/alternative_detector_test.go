package steps

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
	uri "hop.top/cite/scheme"
)

// ─── stubs (ObjectStore) ──────────────────────────────────────────────────────

type stubObjectStore struct {
	objects []*storage.KnowledgeObject
}

func (s *stubObjectStore) Create(_ context.Context, obj *storage.KnowledgeObject) error {
	s.objects = append(s.objects, obj)
	return nil
}

func (s *stubObjectStore) Get(_ context.Context, id string) (*storage.KnowledgeObject, error) {
	for _, o := range s.objects {
		if o.ID == id {
			return o, nil
		}
	}
	return nil, io.EOF // sentinel: not found
}

func (s *stubObjectStore) GetByContentHash(_ context.Context, _ string) (*storage.KnowledgeObject, error) {
	return nil, io.EOF
}

func (s *stubObjectStore) GetBySourceKey(_ context.Context, _ string) (*storage.KnowledgeObject, error) {
	return nil, nil
}

func (s *stubObjectStore) List(_ context.Context, filter storage.ObjectFilter) ([]*storage.KnowledgeObject, int, error) {
	var out []*storage.KnowledgeObject
	for _, o := range s.objects {
		if filter.Type != "" && o.Type != filter.Type {
			continue
		}
		out = append(out, o)
		if filter.Limit > 0 && len(out) >= filter.Limit {
			break
		}
	}
	return out, len(out), nil
}

func (s *stubObjectStore) Update(_ context.Context, _ *storage.KnowledgeObject) error { return nil }
func (s *stubObjectStore) Delete(_ context.Context, _ string) error                   { return nil }
func (s *stubObjectStore) ListBySQL(_ context.Context, _ string, _ []any, _, _ int) ([]*storage.KnowledgeObject, int, error) {
	return nil, 0, nil
}

func (s *stubObjectStore) Reinforce(_ context.Context, _ string, _ *storage.KnowledgeObject) (string, error) {
	return "", nil
}

func (s *stubObjectStore) ListWithEmbeddings(_ context.Context) ([]*storage.KnowledgeObject, error) {
	return nil, nil
}

func (s *stubObjectStore) ListWithoutEmbeddings(_ context.Context) ([]*storage.KnowledgeObject, error) {
	return nil, nil
}

func (s *stubObjectStore) SetReminder(_ context.Context, _ string, _ time.Time) error { return nil }
func (s *stubObjectStore) ClearReminder(_ context.Context, _ string) error             { return nil }
func (s *stubObjectStore) ListDueReminders(_ context.Context, _ time.Time) ([]*storage.KnowledgeObject, error) {
	return nil, nil
}
func (s *stubObjectStore) MarkReminded(_ context.Context, _ string, _ time.Time) error { return nil }
func (s *stubObjectStore) ListPendingReminders(_ context.Context) ([]*storage.KnowledgeObject, error) {
	return nil, nil
}
func (s *stubObjectStore) VectorSearch(_ context.Context, _ []float32, _ storage.ObjectFilter) ([]*storage.KnowledgeObject, error) {
	return nil, nil
}
func (s *stubObjectStore) FTSSearch(_ context.Context, _ string, _ storage.ObjectFilter) ([]*storage.KnowledgeObject, error) {
	return nil, nil
}
func (s *stubObjectStore) FTSSearchNodeAware(_ context.Context, _ string, _ storage.ObjectFilter, _ pluginapi.NodeAwareFilter) ([]*pluginapi.NodeAwareResult, error) {
	return nil, nil
}
func (s *stubObjectStore) VectorSearchNodeAware(_ context.Context, _ []float32, _ storage.ObjectFilter, _ pluginapi.NodeAwareFilter) ([]*pluginapi.NodeAwareResult, error) {
	return nil, nil
}

// ─── stubEdgeStoreAlt (tracks created edges + simulates existing) ─────────────

type stubEdgeStoreAlt struct {
	created  []*storage.Edge
	existing []*storage.Edge // pre-seeded "already existing" edges
}

func (s *stubEdgeStoreAlt) Create(_ context.Context, e *storage.Edge) error {
	s.created = append(s.created, e)
	return nil
}

func (s *stubEdgeStoreAlt) ListFrom(_ context.Context, _, _ string) ([]*storage.Edge, error) {
	return s.existing, nil
}

func (s *stubEdgeStoreAlt) ListTo(_ context.Context, _, _ string) ([]*storage.Edge, error) {
	return nil, nil
}

func (s *stubEdgeStoreAlt) Delete(_ context.Context, _ string) error { return nil }

func (s *stubEdgeStoreAlt) DeleteByObject(_ context.Context, _ string) error { return nil }

func (s *stubEdgeStoreAlt) CountMentionsTo(_ context.Context, _, _ string) (int, error) {
	return 0, nil
}

func (s *stubEdgeStoreAlt) RelatedObjectIDs(_ context.Context, _ string, _, _ int) ([]string, error) {
	return nil, nil
}

// ─── helpers ──────────────────────────────────────────────────────────────────

func makeTags(labels ...string) []storage.Tag {
	tags := make([]storage.Tag, len(labels))
	for i, l := range labels {
		tags[i] = storage.Tag{Label: l, Weight: 1.0}
	}
	return tags
}

func makeURI(t *testing.T, s string) uri.URI {
	t.Helper()
	u, err := uri.Parse(s)
	if err != nil {
		t.Fatalf("parse URI %q: %v", s, err)
	}
	return *u
}

// ─── unit tests ──────────────────────────────────────────────────────────────

func TestAlternativeDetector_Name(t *testing.T) {
	d := NewAlternativeDetector()
	if d.Name() != "alternative_detector" {
		t.Errorf("expected name %q, got %q", "alternative_detector", d.Name())
	}
}

func TestAlternativeDetector_Contract(t *testing.T) {
	d := NewAlternativeDetector()
	c := d.Contract()
	if len(c.Requires) == 0 {
		t.Error("contract must declare at least one required field")
	}
	if len(c.Produces) == 0 {
		t.Error("contract must declare at least one produced field")
	}
}

func TestAlternativeDetector_NilStores_Noop(t *testing.T) {
	d := NewAlternativeDetector()
	draft := &storage.KnowledgeObject{
		ID:   "obj-1",
		Type: "url",
		Tags: makeTags("database", "sql"),
	}
	got, err := d.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Metadata != nil && got.Metadata["alternative_ids"] != nil {
		t.Errorf("expected no alternative_ids in noop mode, got %v", got.Metadata["alternative_ids"])
	}
}

func TestAlternativeDetector_EmptyID_Noop(t *testing.T) {
	objs := &stubObjectStore{}
	edges := &stubEdgeStoreAlt{}
	d := NewAlternativeDetectorWithStores(objs, edges)

	draft := &storage.KnowledgeObject{
		// ID is empty — should skip
		Type: "url",
		Tags: makeTags("database"),
	}
	_, err := d.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(edges.created) != 0 {
		t.Errorf("expected 0 edges for empty-ID draft, got %d", len(edges.created))
	}
}

func TestAlternativeDetector_HighSimilarity_CreatesEdge(t *testing.T) {
	now := time.Now()
	// Two similar tool docs: same type, overlapping tags and mentions.
	candidate := &storage.KnowledgeObject{
		ID:      "cand-1",
		Type:    "url",
		Subtype: "tool",
		Tags:    makeTags("database", "sql", "postgres"),
		Mentions: []uri.URI{
			makeURI(t, "ctxt://entity/vendor/postgres"),
		},
		CreatedAt: now,
	}
	objs := &stubObjectStore{objects: []*storage.KnowledgeObject{candidate}}
	edges := &stubEdgeStoreAlt{}
	d := NewAlternativeDetectorWithStores(objs, edges)

	draft := &storage.KnowledgeObject{
		ID:      "draft-1",
		Type:    "url",
		Subtype: "tool",
		Tags:    makeTags("database", "sql", "mysql"),
		Mentions: []uri.URI{
			makeURI(t, "ctxt://entity/vendor/postgres"),
		},
		CreatedAt: now,
	}

	_, err := d.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(edges.created) == 0 {
		t.Fatal("expected at least one alternative edge for highly similar objects")
	}
	e := edges.created[0]
	if e.EdgeType != "alternative" {
		t.Errorf("expected EdgeType %q, got %q", "alternative", e.EdgeType)
	}
	if e.FromID != "draft-1" || e.ToID != "cand-1" {
		t.Errorf("unexpected edge endpoints: from=%s to=%s", e.FromID, e.ToID)
	}
	if e.Weight <= 0 || e.Weight > 1 {
		t.Errorf("edge weight out of [0,1]: %f", e.Weight)
	}
}

func TestAlternativeDetector_LowSimilarity_NoEdge(t *testing.T) {
	candidate := &storage.KnowledgeObject{
		ID:   "cand-2",
		Type: "url",
		Tags: makeTags("cooking", "recipes"),
	}
	objs := &stubObjectStore{objects: []*storage.KnowledgeObject{candidate}}
	edges := &stubEdgeStoreAlt{}
	d := NewAlternativeDetectorWithStores(objs, edges)

	draft := &storage.KnowledgeObject{
		ID:   "draft-2",
		Type: "url",
		Tags: makeTags("database", "sql", "indexes"),
	}
	_, err := d.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(edges.created) != 0 {
		t.Errorf("expected 0 edges for dissimilar objects, got %d", len(edges.created))
	}
}

func TestAlternativeDetector_SkipSelf(t *testing.T) {
	draft := &storage.KnowledgeObject{
		ID:   "obj-self",
		Type: "url",
		Tags: makeTags("database", "sql"),
	}
	objs := &stubObjectStore{objects: []*storage.KnowledgeObject{draft}}
	edges := &stubEdgeStoreAlt{}
	d := NewAlternativeDetectorWithStores(objs, edges)

	_, err := d.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(edges.created) != 0 {
		t.Errorf("expected 0 edges when only candidate is self, got %d", len(edges.created))
	}
}

func TestAlternativeDetector_SkipExistingEdge(t *testing.T) {
	candidate := &storage.KnowledgeObject{
		ID:   "cand-3",
		Type: "url",
		Tags: makeTags("database", "sql", "postgres"),
	}
	objs := &stubObjectStore{objects: []*storage.KnowledgeObject{candidate}}
	existingEdge := &storage.Edge{
		ID:       "existing-edge",
		FromType: "object",
		FromID:   "draft-3",
		ToType:   "object",
		ToID:     "cand-3",
		EdgeType: "alternative",
		Weight:   0.8,
	}
	edges := &stubEdgeStoreAlt{existing: []*storage.Edge{existingEdge}}
	d := NewAlternativeDetectorWithStores(objs, edges)

	draft := &storage.KnowledgeObject{
		ID:   "draft-3",
		Type: "url",
		Tags: makeTags("database", "sql", "mysql"),
	}
	_, err := d.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(edges.created) != 0 {
		t.Errorf("expected 0 new edges when duplicate already exists, got %d", len(edges.created))
	}
}

func TestAlternativeDetector_MetadataPopulated(t *testing.T) {
	candidate := &storage.KnowledgeObject{
		ID:   "cand-meta",
		Type: "document",
		Tags: makeTags("kubernetes", "containers", "devops"),
	}
	objs := &stubObjectStore{objects: []*storage.KnowledgeObject{candidate}}
	edges := &stubEdgeStoreAlt{}
	d := NewAlternativeDetectorWithStores(objs, edges)

	draft := &storage.KnowledgeObject{
		ID:   "draft-meta",
		Type: "document",
		Tags: makeTags("kubernetes", "containers", "helm"),
	}
	got, err := d.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(edges.created) > 0 {
		ids, ok := got.Metadata["alternative_ids"]
		if !ok {
			t.Error("expected alternative_ids in metadata after edge creation")
		}
		list, ok := ids.([]string)
		if !ok || len(list) == 0 {
			t.Errorf("expected non-empty alternative_ids list, got %v", ids)
		}
	}
}

// ─── scoring unit tests ───────────────────────────────────────────────────────

func TestJaccardTags_FullOverlap(t *testing.T) {
	a := &storage.KnowledgeObject{Tags: makeTags("go", "backend")}
	b := &storage.KnowledgeObject{Tags: makeTags("go", "backend")}
	got := jaccardTags(a, b)
	if got < 0.999 || got > 1.001 {
		t.Errorf("expected ~1.0, got %f", got)
	}
}

func TestJaccardTags_NoOverlap(t *testing.T) {
	a := &storage.KnowledgeObject{Tags: makeTags("go", "backend")}
	b := &storage.KnowledgeObject{Tags: makeTags("rust", "embedded")}
	got := jaccardTags(a, b)
	if got != 0.0 {
		t.Errorf("expected 0.0, got %f", got)
	}
}

func TestJaccardTags_PartialOverlap(t *testing.T) {
	a := &storage.KnowledgeObject{Tags: makeTags("go", "backend", "api")}
	b := &storage.KnowledgeObject{Tags: makeTags("go", "frontend", "react")}
	got := jaccardTags(a, b)
	// intersection=1, union=5 => 0.2
	if got < 0.19 || got > 0.21 {
		t.Errorf("expected ~0.2, got %f", got)
	}
}

func TestJaccardTags_BothEmpty(t *testing.T) {
	a := &storage.KnowledgeObject{}
	b := &storage.KnowledgeObject{}
	got := jaccardTags(a, b)
	if got != 0.0 {
		t.Errorf("expected 0.0 for both empty, got %f", got)
	}
}

func TestCategoryScore_SameTypeAndSubtype(t *testing.T) {
	a := &storage.KnowledgeObject{Type: "url", Subtype: "tool"}
	b := &storage.KnowledgeObject{Type: "url", Subtype: "tool"}
	got := categoryScore(a, b)
	if got < 0.999 {
		t.Errorf("expected 1.0 for same type+subtype, got %f", got)
	}
}

func TestCategoryScore_SameTypeOnly(t *testing.T) {
	a := &storage.KnowledgeObject{Type: "url", Subtype: "tool"}
	b := &storage.KnowledgeObject{Type: "url", Subtype: "article"}
	got := categoryScore(a, b)
	if got < 0.49 || got > 0.51 {
		t.Errorf("expected 0.5 for same type, different subtype, got %f", got)
	}
}

func TestCategoryScore_DifferentType(t *testing.T) {
	a := &storage.KnowledgeObject{Type: "url"}
	b := &storage.KnowledgeObject{Type: "document"}
	got := categoryScore(a, b)
	if got != 0.0 {
		t.Errorf("expected 0.0 for different type, got %f", got)
	}
}

func TestAlternativeEdgeExists_Found(t *testing.T) {
	edges := []*storage.Edge{
		{EdgeType: "mentions", ToID: "obj-a"},
		{EdgeType: "alternative", ToID: "obj-b"},
	}
	if !alternativeEdgeExists(edges, "obj-b") {
		t.Error("expected to find existing alternative edge")
	}
}

func TestAlternativeEdgeExists_NotFound(t *testing.T) {
	edges := []*storage.Edge{
		{EdgeType: "mentions", ToID: "obj-b"},
	}
	if alternativeEdgeExists(edges, "obj-b") {
		t.Error("mentions edge should not count as alternative")
	}
}

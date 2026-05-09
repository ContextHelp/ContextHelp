package promote

import (
	"context"
	"errors"
	"testing"
	"time"

	"hop.top/kit/go/runtime/domain"
	"hop.top/kit/go/runtime/policy"
	"hop.top/kit/go/runtime/policy/withcel"

	"github.com/ideacrafterslabs/ctxt/internal/lateral/lifecycle"
)

// TODO: replace with kit's domain.MockRepository (kit/runtime/domain) once the
// real adapter lands; the fake will desynchronise otherwise.
type fakeStore struct {
	records map[string]map[string]any
	edges   []map[string]string
}

func newFakeStore() *fakeStore {
	return &fakeStore{records: map[string]map[string]any{}}
}

func (s *fakeStore) ReadCandidate(_ context.Context, id string) (map[string]any, error) {
	r, ok := s.records[id]
	if !ok {
		return nil, ErrNotFound
	}
	return r, nil
}

func (s *fakeStore) UpdateCandidate(_ context.Context, id string, patch map[string]any) error {
	r := s.records[id]
	for k, v := range patch {
		r[k] = v
	}
	return nil
}

func (s *fakeStore) WriteEdge(_ context.Context, from, to, typ string) error {
	s.edges = append(s.edges, map[string]string{"from": from, "to": to, "type": typ})
	return nil
}

func (s *fakeStore) KickCanonicalCapture(_ context.Context, url string) (string, error) {
	return "o-canonical-" + url, nil
}

func newEngine(t *testing.T) *policy.Engine {
	t.Helper()
	cfg, err := policy.LoadConfig("policies.yaml")
	if err != nil {
		t.Fatalf("load policies: %v", err)
	}
	eng, err := withcel.New(cfg)
	if err != nil {
		t.Fatalf("withcel.New: %v", err)
	}
	return eng
}

func TestPromote_P2Explicit(t *testing.T) {
	st := newFakeStore()
	st.records["o-lc-1"] = map[string]any{
		"source": "https://x/y",
		"metadata": map[string]any{
			"lifecycle":     map[string]any{"state": "probationary"},
			"discovered_by": "o-parent",
		},
	}
	h := New(st, newEngine(t))
	out, err := h.Promote(context.Background(), "o-lc-1", PathP2Explicit)
	if err != nil {
		t.Fatal(err)
	}
	if out.NewCanonicalID == "" {
		t.Fatal("expected new canonical")
	}
	rec := st.records["o-lc-1"]
	state := rec["metadata"].(map[string]any)["lifecycle"].(map[string]any)["state"]
	if state != string(lifecycle.Promoted) {
		t.Fatalf("expected promoted state, got %v", state)
	}
}

func TestPromote_PreservesSiblingMetadata(t *testing.T) {
	st := newFakeStore()
	st.records["o-lc-2"] = map[string]any{
		"source": "https://x/y",
		"metadata": map[string]any{
			"lifecycle":      map[string]any{"state": "probationary"},
			"kind":           "lateral_candidate",
			"candidate_type": "sibling_repo",
			"discovered_by":  "o-parent",
			"strategy":       "github.owner",
			"scoring":        map[string]any{"score": 0.7, "signals_used": []string{"session_topic"}},
			"preview":        map[string]any{"title": "y"},
		},
	}
	h := New(st, newEngine(t))
	if _, err := h.Promote(context.Background(), "o-lc-2", PathP2Explicit); err != nil {
		t.Fatal(err)
	}
	rec := st.records["o-lc-2"]
	meta := rec["metadata"].(map[string]any)
	for _, k := range []string{"kind", "candidate_type", "discovered_by", "strategy", "scoring", "preview"} {
		if _, ok := meta[k]; !ok {
			t.Fatalf("metadata.%s was wiped during promotion", k)
		}
	}
	lc := meta["lifecycle"].(map[string]any)
	if lc["state"] != string(lifecycle.Promoted) {
		t.Fatalf("expected promoted state, got %v", lc["state"])
	}
	if lc["promotion_path"] != string(PathP2Explicit) {
		t.Fatalf("expected promotion_path=p2_explicit, got %v", lc["promotion_path"])
	}
	if _, ok := lc["promoted_at"].(string); !ok {
		t.Fatal("expected promoted_at to be a string timestamp")
	}
}

func TestPromote_RefuseExpiredBeyondWindow(t *testing.T) {
	st := newFakeStore()
	st.records["o-lc-3"] = map[string]any{
		"source": "https://x/old",
		"metadata": map[string]any{
			"lifecycle": map[string]any{
				"state":      "expired",
				"expired_at": time.Now().Add(-31 * 24 * time.Hour).Format(time.RFC3339),
			},
		},
	}
	h := New(st, newEngine(t))
	_, err := h.Promote(context.Background(), "o-lc-3", PathP2Explicit)
	if err == nil {
		t.Fatal("expected denial for past-window resurrection")
	}
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("expected wrapped ErrConflict, got %v", err)
	}
}

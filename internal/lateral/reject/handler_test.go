package reject

import (
	"context"
	"errors"
	"testing"

	"hop.top/kit/go/runtime/domain"
	"hop.top/kit/go/runtime/policy"
	"hop.top/kit/go/runtime/policy/withcel"
)

// TODO: replace with kit's domain.MockRepository (kit/runtime/domain) once the
// real adapter lands; the fake will desynchronise otherwise.
type fakeStore struct {
	records map[string]map[string]any
	patches map[string]map[string]any
	labels  []string
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		records: map[string]map[string]any{},
		patches: map[string]map[string]any{},
	}
}

func (s *fakeStore) ReadCandidate(_ context.Context, id string) (map[string]any, error) {
	r, ok := s.records[id]
	if !ok {
		return nil, errors.New("not found")
	}
	return r, nil
}

func (s *fakeStore) UpdateCandidate(_ context.Context, id string, patch map[string]any) error {
	s.patches[id] = patch
	// Apply shallow merge into records so sibling-metadata test can read post-state.
	r := s.records[id]
	for k, v := range patch {
		r[k] = v
	}
	return nil
}

func (s *fakeStore) RecordHardNegative(_ context.Context, candidateID string) error {
	s.labels = append(s.labels, candidateID)
	return nil
}

func newEngine(t *testing.T) *policy.Engine {
	t.Helper()
	cfg, err := policy.LoadConfig("policies.yaml")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	eng, err := withcel.New(cfg)
	if err != nil {
		t.Fatalf("withcel.New: %v", err)
	}
	return eng
}

func seedProbationary(st *fakeStore, id string) {
	st.records[id] = map[string]any{
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
}

func TestReject_MarksExpiredAndLabels(t *testing.T) {
	st := newFakeStore()
	seedProbationary(st, "o-lc-1")
	h := New(st, newEngine(t))
	if err := h.Reject(context.Background(), "o-lc-1", "user_rejected"); err != nil {
		t.Fatal(err)
	}
	if _, ok := st.patches["o-lc-1"]; !ok {
		t.Fatal("expected candidate update")
	}
	if len(st.labels) != 1 || st.labels[0] != "o-lc-1" {
		t.Fatalf("expected hard-negative label, got %v", st.labels)
	}
	rec := st.records["o-lc-1"]
	state := rec["metadata"].(map[string]any)["lifecycle"].(map[string]any)["state"]
	if state != "expired" {
		t.Fatalf("expected expired state after reject, got %v", state)
	}
}

func TestReject_RefusesAlreadyPromoted(t *testing.T) {
	st := newFakeStore()
	seedProbationary(st, "o-lc-2")
	// flip to promoted
	st.records["o-lc-2"]["metadata"].(map[string]any)["lifecycle"].(map[string]any)["state"] = "promoted"
	h := New(st, newEngine(t))
	err := h.Reject(context.Background(), "o-lc-2", "user_rejected")
	if err == nil {
		t.Fatal("expected denial for already-promoted candidate")
	}
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("expected wrapped ErrConflict, got %v", err)
	}
}

func TestReject_RefusesEmptyReason(t *testing.T) {
	st := newFakeStore()
	seedProbationary(st, "o-lc-3")
	h := New(st, newEngine(t))
	err := h.Reject(context.Background(), "o-lc-3", "")
	if err == nil {
		t.Fatal("expected denial for empty reason")
	}
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("expected wrapped ErrConflict, got %v", err)
	}
}

func TestReject_PreservesSiblingMetadata(t *testing.T) {
	st := newFakeStore()
	seedProbationary(st, "o-lc-4")
	h := New(st, newEngine(t))
	if err := h.Reject(context.Background(), "o-lc-4", "user_rejected"); err != nil {
		t.Fatal(err)
	}
	rec := st.records["o-lc-4"]
	meta := rec["metadata"].(map[string]any)
	for _, k := range []string{"kind", "candidate_type", "discovered_by", "strategy", "scoring", "preview"} {
		if _, ok := meta[k]; !ok {
			t.Fatalf("metadata.%s was wiped during reject", k)
		}
	}
	lc := meta["lifecycle"].(map[string]any)
	if lc["state"] != "expired" {
		t.Fatalf("expected expired state, got %v", lc["state"])
	}
	if lc["reason"] != "user_rejected" {
		t.Fatalf("expected reason=user_rejected, got %v", lc["reason"])
	}
	if _, ok := lc["expired_at"].(string); !ok {
		t.Fatal("expected expired_at to be a string timestamp")
	}
}

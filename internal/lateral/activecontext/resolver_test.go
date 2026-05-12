package activecontext

import (
	"context"
	"testing"

	lateral "github.com/ideacrafterslabs/ctxt/internal/lateral"
)

type (
	stubSession  struct{ topic map[string]float64 }
	stubWindow   struct{ agg map[string]float64 }
	stubInterest struct{ regs map[string]float64 }
)

func (s *stubSession) DerivedTopic(_ context.Context) (map[string]float64, error) {
	return s.topic, nil
}

func (w *stubWindow) Aggregate(_ context.Context) (map[string]float64, error) { return w.agg, nil }

func (i *stubInterest) ActiveTags(_ context.Context) (map[string]float64, error) { return i.regs, nil }

func TestResolver_AllSignalsPresent(t *testing.T) {
	r := NewResolver(
		&stubSession{topic: map[string]float64{"go": 1.0}},
		&stubWindow{agg: map[string]float64{"cli": 0.7}},
		&stubInterest{regs: map[string]float64{"oss": 0.4}},
	)
	ac, err := r.Resolve(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if ac.Fingerprint == "" {
		t.Fatal("expected non-empty fingerprint")
	}
	if len(ac.SessionTopic) == 0 || len(ac.CaptureWindow) == 0 || len(ac.InterestRegistry) == 0 {
		t.Fatalf("expected all signals populated, got %+v", ac)
	}
}

func TestResolver_MissingSignalIsNil(t *testing.T) {
	r := NewResolver(
		&stubSession{topic: nil},
		&stubWindow{agg: map[string]float64{"cli": 0.7}},
		&stubInterest{regs: nil},
	)
	ac, err := r.Resolve(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if ac.SessionTopic != nil {
		t.Fatal("expected nil session topic")
	}
	if ac.InterestRegistry != nil {
		t.Fatal("expected nil interest registry")
	}
	if ac.CaptureWindow == nil {
		t.Fatal("expected capture window populated")
	}
}

func TestResolver_FingerprintIsStable(t *testing.T) {
	r1 := NewResolver(
		&stubSession{topic: map[string]float64{"go": 1.0}},
		&stubWindow{agg: map[string]float64{"cli": 0.7}},
		&stubInterest{regs: map[string]float64{"oss": 0.4}},
	)
	r2 := NewResolver(
		&stubSession{topic: map[string]float64{"go": 1.0}},
		&stubWindow{agg: map[string]float64{"cli": 0.7}},
		&stubInterest{regs: map[string]float64{"oss": 0.4}},
	)
	ac1, _ := r1.Resolve(context.Background())
	ac2, _ := r2.Resolve(context.Background())
	if ac1.Fingerprint != ac2.Fingerprint {
		t.Fatalf("expected stable fingerprint, got %q vs %q", ac1.Fingerprint, ac2.Fingerprint)
	}
}

// TestFingerprint_AuthorHintsAffectHash pins that the fingerprint
// participates in cache-key dedup correctly: two ACs with identical
// signal maps but different AuthorHints MUST hash to different
// fingerprints. Without this, github author_other_pr probes (which gate
// on AuthorHints) would share a cache entry across distinct authors.
func TestFingerprint_AuthorHintsAffectHash(t *testing.T) {
	base := lateral.ActiveContext{
		SessionTopic:     map[string]float64{"go": 1.0},
		CaptureWindow:    map[string]float64{"cli": 0.7},
		InterestRegistry: map[string]float64{"oss": 0.4},
	}
	withSamber := base
	withSamber.AuthorHints = map[string]string{"github": "samber"}
	withJadb := base
	withJadb.AuthorHints = map[string]string{"github": "jadb"}

	if Fingerprint(withSamber) == Fingerprint(withJadb) {
		t.Fatal("fingerprint identical for different AuthorHints; cache-key collision")
	}
	if Fingerprint(base) == Fingerprint(withSamber) {
		t.Fatal("fingerprint identical with vs without AuthorHints; cache-key collision")
	}
}

// TestFingerprint_AuthorHintsStableAcrossNilAndEmpty confirms nil and
// empty AuthorHints maps produce the same fingerprint (canonical empty),
// so a daemon that initializes AuthorHints as an empty map vs leaving it
// nil doesn't accidentally invalidate caches.
func TestFingerprint_AuthorHintsStableAcrossNilAndEmpty(t *testing.T) {
	withNil := lateral.ActiveContext{
		SessionTopic: map[string]float64{"go": 1.0},
	}
	withEmpty := lateral.ActiveContext{
		SessionTopic: map[string]float64{"go": 1.0},
		AuthorHints:  map[string]string{},
	}
	if Fingerprint(withNil) != Fingerprint(withEmpty) {
		t.Fatalf("nil vs empty AuthorHints produced different fingerprints: %q vs %q",
			Fingerprint(withNil), Fingerprint(withEmpty))
	}
}

// TestFingerprint_AuthorHintsKeyOrderDeterministic pins that AuthorHints
// keys are sorted before hashing — Go map iteration is unordered, so a
// naive implementation would produce different hashes on different runs.
func TestFingerprint_AuthorHintsKeyOrderDeterministic(t *testing.T) {
	ac := lateral.ActiveContext{
		AuthorHints: map[string]string{
			"github":   "samber",
			"x":        "twitter_handle",
			"linkedin": "linkedin_slug",
		},
	}
	first := Fingerprint(ac)
	for i := 0; i < 50; i++ {
		if got := Fingerprint(ac); got != first {
			t.Fatalf("fingerprint non-deterministic at iter %d: %q vs %q", i, got, first)
		}
	}
}

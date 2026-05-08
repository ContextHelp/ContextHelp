package activecontext

import (
	"context"
	"testing"
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

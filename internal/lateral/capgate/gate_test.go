package capgate

import "testing"

func TestGate_TopKAndThreshold(t *testing.T) {
	cfg := Config{
		ThresholdsByType: map[string]float64{"sibling_repo": 0.45},
		CapKByType:       map[string]int{"sibling_repo": 2},
	}
	g := NewGate(cfg)

	in := []Scored{
		{Type: "sibling_repo", Score: 0.9, URL: "a"},
		{Type: "sibling_repo", Score: 0.8, URL: "b"},
		{Type: "sibling_repo", Score: 0.5, URL: "c"},
		{Type: "sibling_repo", Score: 0.3, URL: "d"},
	}
	out := g.Filter(in)
	if len(out) != 2 {
		t.Fatalf("expected top 2 above threshold, got %d", len(out))
	}
	if out[0].URL != "a" || out[1].URL != "b" {
		t.Fatalf("expected [a,b], got %v", urls(out))
	}
}

func TestGate_SingletonBypassesK(t *testing.T) {
	cfg := Config{
		ThresholdsByType: map[string]float64{"owner_profile": 0.0, "sibling_repo": 0.45},
		CapKByType:       map[string]int{"sibling_repo": 1},
		Singletons:       map[string]bool{"owner_profile": true, "sponsor_page": true},
	}
	g := NewGate(cfg)
	in := []Scored{
		{Type: "owner_profile", Score: 0.05, URL: "owner"},
		{Type: "sibling_repo", Score: 0.6, URL: "sib1"},
		{Type: "sibling_repo", Score: 0.5, URL: "sib2"},
	}
	out := g.Filter(in)
	if len(out) != 2 {
		t.Fatalf("expected owner + 1 sibling, got %d", len(out))
	}
}

func urls(s []Scored) []string {
	out := make([]string, len(s))
	for i, x := range s {
		out[i] = x.URL
	}
	return out
}

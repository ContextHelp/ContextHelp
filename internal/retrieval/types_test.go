package retrieval

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/search"
)

func TestEffectiveTopK_ZeroMultiplier(t *testing.T) {
	tc := TierConfig{Enabled: true, TopK: 10}
	// mode with no entry in map → multiplier 0 → treated as 1.0 → returns TopK unchanged
	got := tc.EffectiveTopK(search.QueryModeDefault)
	if got != 10 {
		t.Errorf("EffectiveTopK with zero multiplier: got %d, want 10", got)
	}
}

func TestEffectiveTopK_KeywordOnTopK10(t *testing.T) {
	tc := TierConfig{
		Enabled: true,
		TopK:    10,
		TopKMultipliers: map[search.QueryMode]float64{
			search.QueryModeKeyword: 0.5,
		},
	}
	got := tc.EffectiveTopK(search.QueryModeKeyword)
	if got != 5 {
		t.Errorf("EffectiveTopK(keyword, topK=10, mult=0.5): got %d, want 5", got)
	}
}

func TestEffectiveTopK_MinimumOne(t *testing.T) {
	tc := TierConfig{
		Enabled: true,
		TopK:    1,
		TopKMultipliers: map[search.QueryMode]float64{
			search.QueryModeKeyword: 0.1,
		},
	}
	got := tc.EffectiveTopK(search.QueryModeKeyword)
	if got < 1 {
		t.Errorf("EffectiveTopK must be at least 1, got %d", got)
	}
}

func TestDefaultConfig_Multipliers(t *testing.T) {
	cfg := DefaultConfig()

	tests := []struct {
		name string
		tier TierConfig
		mode search.QueryMode
		want int
	}{
		// Categories TopK=10
		{name: "categories/keyword", tier: cfg.Categories, mode: search.QueryModeKeyword, want: 5},
		{name: "categories/question", tier: cfg.Categories, mode: search.QueryModeQuestion, want: 15},
		{name: "categories/technical", tier: cfg.Categories, mode: search.QueryModeTechnical, want: 12},
		{name: "categories/default", tier: cfg.Categories, mode: search.QueryModeDefault, want: 10},
		// Items TopK=20
		{name: "items/keyword", tier: cfg.Items, mode: search.QueryModeKeyword, want: 10},
		{name: "items/question", tier: cfg.Items, mode: search.QueryModeQuestion, want: 30},
		{name: "items/technical", tier: cfg.Items, mode: search.QueryModeTechnical, want: 25},
		{name: "items/default", tier: cfg.Items, mode: search.QueryModeDefault, want: 20},
		// Resources TopK=5
		{name: "resources/keyword", tier: cfg.Resources, mode: search.QueryModeKeyword, want: 5},
		{name: "resources/question", tier: cfg.Resources, mode: search.QueryModeQuestion, want: 10},
		{name: "resources/technical", tier: cfg.Resources, mode: search.QueryModeTechnical, want: 7},
		{name: "resources/default", tier: cfg.Resources, mode: search.QueryModeDefault, want: 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.tier.EffectiveTopK(tt.mode)
			if got != tt.want {
				t.Errorf("EffectiveTopK(%q): got %d, want %d", tt.mode, got, tt.want)
			}
		})
	}
}

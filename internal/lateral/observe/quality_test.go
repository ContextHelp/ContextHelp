package observe

import (
	"math"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/lateral/eval"
)

func TestCompute_WeightedAverage(t *testing.T) {
	t.Parallel()

	metrics := eval.Report{
		Strategies: map[string]eval.Metrics{
			"github": {StrategyID: "github", Precision: 1.0, Recall: 0.8, Sample: 10},
			"jit":    {StrategyID: "jit", Precision: 0.5, Recall: 0.5, Sample: 4},
		},
	}
	conversion := map[string]eval.ConversionRate{
		"github": {StrategyID: "github", Rate: 0.5, Emitted: 10, Promoted: 5},
		"jit":    {StrategyID: "jit", Rate: 0.0, Emitted: 4, Promoted: 0},
	}

	got := Compute(metrics, conversion, DefaultQualityWeights())
	if len(got) != 2 {
		t.Fatalf("len = %d; want 2", len(got))
	}
	gh := got[0]
	if gh.StrategyID != "github" {
		t.Fatalf("first = %q; want github (sorted)", gh.StrategyID)
	}
	want := 0.5*1.0 + 0.5*0.5
	if math.Abs(gh.Score-want) > 1e-9 {
		t.Errorf("github score = %v; want %v", gh.Score, want)
	}
	jit := got[1]
	wantJit := 0.5*0.5 + 0.5*0.0
	if math.Abs(jit.Score-wantJit) > 1e-9 {
		t.Errorf("jit score = %v; want %v", jit.Score, wantJit)
	}
}

func TestCompute_HandlesMissingAxis(t *testing.T) {
	t.Parallel()

	// Strategy in conversion only — precision side defaults to zero.
	metrics := eval.Report{Strategies: map[string]eval.Metrics{}}
	conversion := map[string]eval.ConversionRate{
		"isolated": {StrategyID: "isolated", Rate: 0.6, Emitted: 10, Promoted: 6},
	}
	got := Compute(metrics, conversion, DefaultQualityWeights())
	if len(got) != 1 || got[0].StrategyID != "isolated" {
		t.Fatalf("got = %+v", got)
	}
	if got[0].Precision != 0 {
		t.Errorf("missing precision should be 0; got %v", got[0].Precision)
	}
	if math.Abs(got[0].Score-0.3) > 1e-9 {
		t.Errorf("score = %v; want 0.3", got[0].Score)
	}
}

func TestCompute_DegenerateWeightsFallBackToHalf(t *testing.T) {
	t.Parallel()

	metrics := eval.Report{
		Strategies: map[string]eval.Metrics{
			"x": {StrategyID: "x", Precision: 1.0},
		},
	}
	conversion := map[string]eval.ConversionRate{
		"x": {StrategyID: "x", Rate: 0.0},
	}
	got := Compute(metrics, conversion, QualityWeights{}) // 0/0
	if math.Abs(got[0].Score-0.5) > 1e-9 {
		t.Errorf("score = %v; want 0.5 (default-fallback)", got[0].Score)
	}
}

func TestCompute_UnequalWeights(t *testing.T) {
	t.Parallel()

	metrics := eval.Report{
		Strategies: map[string]eval.Metrics{
			"x": {StrategyID: "x", Precision: 1.0},
		},
	}
	conversion := map[string]eval.ConversionRate{
		"x": {StrategyID: "x", Rate: 0.0},
	}
	w := QualityWeights{Precision: 3, Conversion: 1}
	got := Compute(metrics, conversion, w)
	want := (3*1.0 + 1*0.0) / 4.0 // 0.75
	if math.Abs(got[0].Score-want) > 1e-9 {
		t.Errorf("score = %v; want %v", got[0].Score, want)
	}
}

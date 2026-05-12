package eval

import (
	"strings"
	"testing"

	lateral "github.com/ideacrafterslabs/ctxt/internal/lateral"
)

func TestComputeMetrics_PerfectScore(t *testing.T) {
	t.Parallel()

	report := ReplayReport{
		Results: []Result{
			{
				Fixture:    Fixture{Expect: FixtureExpectation{StrategyID: "github"}},
				Strategies: []string{"github"},
				Candidates: []lateral.Candidate{{Strategy: "github"}},
			},
			{
				Fixture:    Fixture{Expect: FixtureExpectation{StrategyID: "github"}},
				Strategies: []string{"github"},
			},
			{
				Fixture:    Fixture{Expect: FixtureExpectation{Negative: true}},
				Strategies: nil,
			},
		},
	}
	m := ComputeMetrics(report)
	gh := m.Strategies["github"]
	if gh.Precision != 1.0 {
		t.Errorf("Precision = %v; want 1.0", gh.Precision)
	}
	if gh.Recall != 1.0 {
		t.Errorf("Recall = %v; want 1.0", gh.Recall)
	}
	if gh.NegativePass != 1.0 {
		t.Errorf("NegativePass = %v; want 1.0", gh.NegativePass)
	}
	if gh.Sample != 2 {
		t.Errorf("Sample = %d; want 2", gh.Sample)
	}
	if m.TotalNegative != 1 {
		t.Errorf("TotalNegative = %d; want 1", m.TotalNegative)
	}
}

func TestComputeMetrics_PrecisionMiss(t *testing.T) {
	t.Parallel()

	// Strategy claimed a negative event, lowering precision and
	// negative-pass.
	report := ReplayReport{
		Results: []Result{
			{
				Fixture:    Fixture{Expect: FixtureExpectation{StrategyID: "github"}},
				Strategies: []string{"github"},
			},
			{
				Fixture:    Fixture{Expect: FixtureExpectation{Negative: true}},
				Strategies: []string{"github"},
			},
		},
	}
	m := ComputeMetrics(report)
	gh := m.Strategies["github"]
	if gh.Precision != 0.5 {
		t.Errorf("Precision = %v; want 0.5", gh.Precision)
	}
	if gh.Recall != 1.0 {
		t.Errorf("Recall = %v; want 1.0", gh.Recall)
	}
	if gh.NegativePass != 0.0 {
		t.Errorf("NegativePass = %v; want 0.0", gh.NegativePass)
	}
}

func TestComputeMetrics_RecallMiss(t *testing.T) {
	t.Parallel()

	// Strategy missed a labelled positive — recall drops.
	report := ReplayReport{
		Results: []Result{
			{
				Fixture:    Fixture{Expect: FixtureExpectation{StrategyID: "github"}},
				Strategies: []string{"github"},
			},
			{
				Fixture:    Fixture{Expect: FixtureExpectation{StrategyID: "github"}},
				Strategies: nil, // missed
			},
		},
	}
	m := ComputeMetrics(report)
	gh := m.Strategies["github"]
	if gh.Recall != 0.5 {
		t.Errorf("Recall = %v; want 0.5", gh.Recall)
	}
	if gh.Sample != 2 {
		t.Errorf("Sample = %d; want 2", gh.Sample)
	}
}

func TestRenderMetricsTable_NonEmpty(t *testing.T) {
	t.Parallel()
	r := Report{
		Strategies: map[string]Metrics{
			"github": {StrategyID: "github", Precision: 0.92, Recall: 0.78, NegativePass: 1.0, Sample: 8},
			"jit":    {StrategyID: "jit", Precision: 0.61, Recall: 0.55, NegativePass: 0.9, Sample: 12},
		},
		TotalFixtures: 20,
		TotalPositive: 18,
		TotalNegative: 2,
	}
	out := RenderMetricsTable(r)
	if !strings.Contains(out, "github") || !strings.Contains(out, "jit") {
		t.Errorf("table missing strategies: %s", out)
	}
	if !strings.Contains(out, "0.92") || !strings.Contains(out, "0.55") {
		t.Errorf("table missing values: %s", out)
	}
	if !strings.Contains(out, "20 fixtures") {
		t.Errorf("missing dataset summary: %s", out)
	}
}

func TestRenderMetricsTable_Empty(t *testing.T) {
	t.Parallel()
	if got := RenderMetricsTable(Report{}); got != "no strategies in report\n" {
		t.Errorf("got %q", got)
	}
}

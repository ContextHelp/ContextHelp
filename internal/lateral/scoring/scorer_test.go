package scoring

import (
	"context"
	"testing"

	lateral "github.com/ideacrafterslabs/ctxt/internal/lateral"
)

type fakeEva struct{ score float64 }

func (f *fakeEva) Cosine(_ context.Context, _ map[string]float64, _ map[string]float64) (float64, error) {
	return f.score, nil
}

func TestScorer_AllSignalsContribute(t *testing.T) {
	s := NewScorer(&fakeEva{score: 0.8}, DefaultWeights())
	ac := lateral.ActiveContext{
		SessionTopic:     map[string]float64{"go": 1},
		CaptureWindow:    map[string]float64{"cli": 1},
		InterestRegistry: map[string]float64{"oss": 1},
	}
	c := lateral.Candidate{URL: "https://example.com", Preview: map[string]any{"topics": []string{"go"}}}
	out, err := s.Score(context.Background(), c, ac)
	if err != nil {
		t.Fatal(err)
	}
	want := 0.8
	if out.Score < want-0.001 || out.Score > want+0.001 {
		t.Fatalf("expected %.3f, got %.3f", want, out.Score)
	}
	if len(out.SignalsUsed) != 3 {
		t.Fatalf("expected 3 signals, got %v", out.SignalsUsed)
	}
}

func TestScorer_MissingSignalRedistributes(t *testing.T) {
	s := NewScorer(&fakeEva{score: 0.6}, DefaultWeights())
	ac := lateral.ActiveContext{
		SessionTopic:  nil,
		CaptureWindow: map[string]float64{"cli": 1},
	}
	c := lateral.Candidate{URL: "https://example.com"}
	out, err := s.Score(context.Background(), c, ac)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.SignalsUsed) != 1 || out.SignalsUsed[0] != "capture_window" {
		t.Fatalf("expected only capture_window, got %v", out.SignalsUsed)
	}
	got := out.WeightsApplied["capture_window"]
	if got < 0.999 || got > 1.001 {
		t.Fatalf("expected redistributed weight 1.0, got %.3f", got)
	}
}

package eval

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	lateral "github.com/ideacrafterslabs/ctxt/internal/lateral"
)

// stubStrategy is a minimal LateralStrategy for the replay tests. It
// claims any event whose SourceURL contains a configured marker and
// emits a fixed candidate set.
type stubStrategy struct {
	id         string
	family     lateral.StrategyFamily
	marker     string // SourceURL must contain this to claim
	emit       []lateral.Candidate
	err        error
	probeCalls int
}

func (s *stubStrategy) ID() string                     { return s.id }
func (s *stubStrategy) Family() lateral.StrategyFamily { return s.family }
func (s *stubStrategy) Applies(_ context.Context, ev lateral.CapturedEvent) lateral.AppliesResult {
	if s.marker == "" || strings.Contains(ev.SourceURL, s.marker) {
		return lateral.AppliesResult{Matches: true, Specificity: 1}
	}
	return lateral.AppliesResult{}
}
func (s *stubStrategy) Probe(_ context.Context, _ lateral.CapturedEvent, _ lateral.ActiveContext) ([]lateral.Candidate, error) {
	s.probeCalls++
	if s.err != nil {
		return nil, s.err
	}
	return s.emit, nil
}
func (*stubStrategy) Preconditions() []string { return nil }

func TestReplayFixtures_DispatchesAndCapturesCandidates(t *testing.T) {
	t.Parallel()

	reg := lateral.NewRegistry()
	gh := &stubStrategy{
		id:     "github",
		family: lateral.FamilyPlatform,
		marker: "github.com",
		emit: []lateral.Candidate{
			{URL: "https://github.com/owner/sibling", Strategy: "github"},
		},
	}
	reg.Register(gh)

	fixtures := []Fixture{
		{
			Event:  lateral.CapturedEvent{SourceURL: "https://github.com/owner/repo"},
			Expect: FixtureExpectation{StrategyID: "github", MinCandidates: 1},
		},
		{
			Event:  lateral.CapturedEvent{SourceURL: "https://example.com/page"},
			Expect: FixtureExpectation{Negative: true},
		},
	}

	rep := ReplayFixtures(context.Background(), reg, fixtures)
	if len(rep.Results) != 2 {
		t.Fatalf("Results = %d; want 2", len(rep.Results))
	}
	if got := rep.Results[0].Strategies; len(got) != 1 || got[0] != "github" {
		t.Errorf("first dispatched strategies = %v; want [github]", got)
	}
	if len(rep.Results[0].Candidates) != 1 {
		t.Errorf("first candidate count = %d; want 1", len(rep.Results[0].Candidates))
	}
	if len(rep.Results[1].Strategies) != 0 {
		t.Errorf("second event should match nothing; got %v", rep.Results[1].Strategies)
	}
}

func TestReplayFixtures_RecordsProbeError(t *testing.T) {
	t.Parallel()

	reg := lateral.NewRegistry()
	failing := &stubStrategy{
		id:     "broken",
		family: lateral.FamilyPlatform,
		marker: "broken",
		err:    errors.New("boom"),
	}
	reg.Register(failing)

	fixtures := []Fixture{
		{Event: lateral.CapturedEvent{SourceURL: "https://broken/x"}},
	}
	rep := ReplayFixtures(context.Background(), reg, fixtures)
	if rep.Results[0].Errors["broken"] != "boom" {
		t.Errorf("Errors[broken] = %q; want boom", rep.Results[0].Errors["broken"])
	}
	if len(rep.Results[0].Candidates) != 0 {
		t.Errorf("Candidates = %d; want 0 on error", len(rep.Results[0].Candidates))
	}
}

func TestDecodeFixtures_HandlesCommentsAndBlankLines(t *testing.T) {
	t.Parallel()

	body := strings.Join([]string{
		"# header comment",
		"",
		`{"event":{"source_url":"https://x"},"expect":{"strategy_id":"x"}}`,
		"# inline comment",
		`{"event":{"source_url":"https://y"},"expect":{"strategy_id":"y"}}`,
	}, "\n")
	got, err := DecodeFixtures(strings.NewReader(body))
	if err != nil {
		t.Fatalf("DecodeFixtures err = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d; want 2", len(got))
	}
	if got[0].Event.SourceURL != "https://x" {
		t.Errorf("first url = %q", got[0].Event.SourceURL)
	}
}

func TestDecodeFixtures_RejectsEmpty(t *testing.T) {
	t.Parallel()
	if _, err := DecodeFixtures(strings.NewReader("")); err == nil {
		t.Fatal("expected error on empty input")
	}
}

func TestLoadFixtures_FromDisk(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "f.jsonl")
	body := `{"event":{"source_url":"https://github.com/o/r"},"expect":{"strategy_id":"github"}}` + "\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	got, err := LoadFixtures(path)
	if err != nil {
		t.Fatalf("LoadFixtures err = %v", err)
	}
	if len(got) != 1 || got[0].Expect.StrategyID != "github" {
		t.Errorf("got = %+v", got)
	}
}

func TestReplay_LoadsAndDispatches(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "f.jsonl")
	body := `{"event":{"source_url":"https://github.com/o/r"},"expect":{"strategy_id":"github","min_candidates":1}}` + "\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	reg := lateral.NewRegistry()
	reg.Register(&stubStrategy{
		id: "github", family: lateral.FamilyPlatform, marker: "github.com",
		emit: []lateral.Candidate{{URL: "https://github.com/o/sibling", Strategy: "github"}},
	})

	rep, err := Replay(context.Background(), reg, path)
	if err != nil {
		t.Fatalf("Replay err = %v", err)
	}
	if len(rep.Results) != 1 {
		t.Fatalf("Results = %d; want 1", len(rep.Results))
	}
}

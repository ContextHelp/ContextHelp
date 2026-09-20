package eval

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	lateral "github.com/ideacrafterslabs/ctxt/internal/lateral"
)

// Fixture is one labelled record from a JSONL fixtures file. The file
// is line-delimited; each line round-trips through encoding/json.
//
// Event is the input the harness dispatches through the registry.
// Expect describes what a "correct" strategy emit looks like for that
// event — used by the metric package to compute precision/recall.
//
// The on-disk shape uses snake_case field names; the eventEnvelope
// type below maps the wire keys onto lateral.CapturedEvent's exported
// fields without polluting the substrate type with JSON tags (the
// substrate is used in many contexts where a stable wire shape would
// over-constrain refactors).
type Fixture struct {
	Event  lateral.CapturedEvent
	Expect FixtureExpectation
}

// eventEnvelope is the JSONL on-disk shape for fixtures. Kept private
// so callers always go through Fixture's MarshalJSON / UnmarshalJSON.
type eventEnvelope struct {
	Event  capturedEventWire  `json:"event"`
	Expect FixtureExpectation `json:"expect"`
}

// capturedEventWire is the snake_case wire shape for lateral.CapturedEvent.
// It is decoded into the substrate type via toCapturedEvent.
type capturedEventWire struct {
	ObjectID        string `json:"object_id"`
	Namespace       string `json:"namespace"`
	SourceURL       string `json:"source_url"`
	CapturePipeline string `json:"capture_pipeline"`
	PersistedAt     int64  `json:"persisted_at"`
	Hints           struct {
		Generator     string `json:"generator"`
		MetaPlatform  string `json:"meta_platform"`
		CanonicalHost string `json:"canonical_host"`
	} `json:"hints"`
}

func (w capturedEventWire) toCapturedEvent() lateral.CapturedEvent {
	return lateral.CapturedEvent{
		ObjectID:        w.ObjectID,
		Namespace:       w.Namespace,
		SourceURL:       w.SourceURL,
		CapturePipeline: w.CapturePipeline,
		PersistedAt:     w.PersistedAt,
		Hints: lateral.Hints{
			Generator:     w.Hints.Generator,
			MetaPlatform:  w.Hints.MetaPlatform,
			CanonicalHost: w.Hints.CanonicalHost,
		},
	}
}

func capturedEventToWire(e lateral.CapturedEvent) capturedEventWire {
	w := capturedEventWire{
		ObjectID:        e.ObjectID,
		Namespace:       e.Namespace,
		SourceURL:       e.SourceURL,
		CapturePipeline: e.CapturePipeline,
		PersistedAt:     e.PersistedAt,
	}
	w.Hints.Generator = e.Hints.Generator
	w.Hints.MetaPlatform = e.Hints.MetaPlatform
	w.Hints.CanonicalHost = e.Hints.CanonicalHost
	return w
}

// MarshalJSON renders Fixture as the eventEnvelope wire shape.
func (f Fixture) MarshalJSON() ([]byte, error) {
	return json.Marshal(eventEnvelope{
		Event:  capturedEventToWire(f.Event),
		Expect: f.Expect,
	})
}

// UnmarshalJSON parses the eventEnvelope wire shape.
func (f *Fixture) UnmarshalJSON(data []byte) error {
	var env eventEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return err
	}
	f.Event = env.Event.toCapturedEvent()
	f.Expect = env.Expect
	return nil
}

// FixtureExpectation captures the labelled outcome for a single
// fixture. All fields are optional; the metric package interprets
// absence as "no expectation on this axis."
//
//   - StrategyID: the strategy the operator expects to claim the event.
//     Empty = any strategy is acceptable. Non-empty + dispatching to a
//     different strategy = precision miss.
//   - Negative: when true, the event MUST emit zero candidates from
//     any strategy. Useful for "we should NOT lateral-discover here"
//     cases (e.g. captures of admin pages that look platform-shaped).
//   - MinCandidates: a lower bound on candidates from StrategyID. Zero
//     skips the check; ignored entirely when Negative=true.
//   - CandidateURLs: canonical URLs the harness expects to see in the
//     emitted candidate set. Order-insensitive; case-sensitive.
type FixtureExpectation struct {
	StrategyID    string   `json:"strategy_id,omitempty"`
	Negative      bool     `json:"negative,omitempty"`
	MinCandidates int      `json:"min_candidates,omitempty"`
	CandidateURLs []string `json:"candidate_urls,omitempty"`
}

// ReplayReport is the structured output of a replay run. Each Result
// pairs the input fixture with what the registry produced, so the
// metric package can compute scores without re-dispatching.
type ReplayReport struct {
	Results []Result `json:"results"`
}

// Result is one event's replay outcome. Strategies is the list of
// strategy IDs the registry dispatched to (zero when no strategy
// claimed). Candidates is the union of every claimed strategy's Probe
// output. Errors records per-strategy failures so the metric package
// can distinguish "strategy emitted nothing" from "strategy crashed."
type Result struct {
	Fixture    Fixture             `json:"fixture"`
	Strategies []string            `json:"strategies"`
	Candidates []lateral.Candidate `json:"candidates"`
	Errors     map[string]string   `json:"errors,omitempty"`
}

// Replay loads fixtures from path and dispatches each event through
// reg, returning a ReplayReport. Errors from a single Probe call are
// attached to the corresponding Result (Errors[strategyID]) — they
// don't abort the run, mirroring the daemon's lifecycle.handleCaptureEvent
// behaviour where one strategy's crash doesn't poison its peers.
//
// path may be "-" to read from stdin.
func Replay(ctx context.Context, reg *lateral.Registry, path string) (ReplayReport, error) {
	fixtures, err := LoadFixtures(path)
	if err != nil {
		return ReplayReport{}, fmt.Errorf("load fixtures: %w", err)
	}
	return ReplayFixtures(ctx, reg, fixtures), nil
}

// ReplayFixtures dispatches each fixture through reg and returns the
// ReplayReport. Pure (no I/O); the file-reading variant Replay layers
// on top.
func ReplayFixtures(ctx context.Context, reg *lateral.Registry, fixtures []Fixture) ReplayReport {
	results := make([]Result, 0, len(fixtures))
	for _, f := range fixtures {
		r := Result{Fixture: f}
		chosen := reg.Dispatch(ctx, f.Event)
		for _, s := range chosen {
			r.Strategies = append(r.Strategies, s.ID())
			out, err := s.Probe(ctx, f.Event, lateral.ActiveContext{})
			if err != nil {
				if r.Errors == nil {
					r.Errors = map[string]string{}
				}
				r.Errors[s.ID()] = err.Error()
				continue
			}
			r.Candidates = append(r.Candidates, out...)
		}
		results = append(results, r)
	}
	return ReplayReport{Results: results}
}

// LoadFixtures reads a JSONL fixture file. path may be "-" to read
// from stdin. Blank lines and lines starting with "#" are skipped so
// fixture authors can comment in-line.
func LoadFixtures(path string) ([]Fixture, error) {
	var rd io.Reader
	if path == "-" {
		rd = os.Stdin
	} else {
		f, err := os.Open(path) // #nosec G304 — operator-supplied fixture path
		if err != nil {
			return nil, err
		}
		defer f.Close()
		rd = f
	}
	return DecodeFixtures(rd)
}

// DecodeFixtures parses JSONL content from rd. Exposed separately so
// callers (tests, CI script that batches multiple files) can decode
// from any io.Reader.
func DecodeFixtures(rd io.Reader) ([]Fixture, error) {
	scanner := bufio.NewScanner(rd)
	// Allow long lines (multi-kilobyte event payloads).
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var out []Fixture
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Bytes()
		if len(line) == 0 || line[0] == '#' {
			continue
		}
		var f Fixture
		if err := json.Unmarshal(line, &f); err != nil {
			return nil, fmt.Errorf("line %d: %w", lineNum, err)
		}
		out = append(out, f)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read: %w", err)
	}
	if len(out) == 0 {
		return nil, errors.New("no fixtures decoded")
	}
	return out, nil
}

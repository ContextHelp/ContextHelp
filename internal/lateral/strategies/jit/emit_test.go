package jit_test

import (
	"errors"
	"testing"

	lateral "github.com/ideacrafterslabs/ctxt/internal/lateral"
	"github.com/ideacrafterslabs/ctxt/internal/lateral/strategies/jit"
)

func TestEmit_EmptyInput(t *testing.T) {
	cands, fails := jit.Emit(nil, "post")
	if cands != nil || fails != nil {
		t.Fatalf("Emit(nil) = (%v, %v), want (nil, nil)", cands, fails)
	}

	cands, fails = jit.Emit([]jit.FetchResult{}, "post")
	if cands != nil || fails != nil {
		t.Fatalf("Emit(empty) = (%v, %v), want (nil, nil)", cands, fails)
	}
}

func TestEmit_AllSuccess(t *testing.T) {
	results := []jit.FetchResult{
		{URL: "https://acme.io/about", Body: "hello"},
		{URL: "https://acme.io/team", Body: ""},
	}
	cands, fails := jit.Emit(results, "post")
	if fails != nil {
		t.Fatalf("Emit failures = %v, want nil", fails)
	}
	if len(cands) != 2 {
		t.Fatalf("Emit candidates = %d, want 2", len(cands))
	}

	for i, c := range cands {
		if c.CandidateType != jit.CandidateType {
			t.Errorf("cands[%d].CandidateType = %q, want %q", i, c.CandidateType, jit.CandidateType)
		}
		if c.Strategy != jit.StrategyID {
			t.Errorf("cands[%d].Strategy = %q, want %q", i, c.Strategy, jit.StrategyID)
		}
		if c.URL != results[i].URL {
			t.Errorf("cands[%d].URL = %q, want %q", i, c.URL, results[i].URL)
		}
		pt, ok := c.Preview["page_type"]
		if !ok || pt != "post" {
			t.Errorf("cands[%d].Preview[page_type] = %v, want %q", i, pt, "post")
		}
		bs, ok := c.Preview["body_size"]
		if !ok || bs != len(results[i].Body) {
			t.Errorf("cands[%d].Preview[body_size] = %v, want %d", i, bs, len(results[i].Body))
		}
	}
}

func TestEmit_FailuresRoutedSeparately(t *testing.T) {
	netErr := errors.New("net down")
	results := []jit.FetchResult{
		{URL: "https://acme.io/good", Body: "ok"},
		{URL: "https://acme.io/bad", Err: netErr},
		{URL: "https://acme.io/also-good", Body: "ok2"},
	}
	cands, fails := jit.Emit(results, "post")

	if len(cands) != 2 {
		t.Fatalf("candidates = %d, want 2 (only successes)", len(cands))
	}
	if cands[0].URL != "https://acme.io/good" || cands[1].URL != "https://acme.io/also-good" {
		t.Errorf("candidate URLs = [%q, %q], want [/good, /also-good]", cands[0].URL, cands[1].URL)
	}

	if len(fails) != 1 {
		t.Fatalf("failures = %d, want 1", len(fails))
	}
	if fails[0].URL != "https://acme.io/bad" {
		t.Errorf("failure URL = %q, want /bad", fails[0].URL)
	}
	if !errors.Is(fails[0].Err, netErr) {
		t.Errorf("failure Err = %v, want %v", fails[0].Err, netErr)
	}
}

func TestEmit_AllFailures(t *testing.T) {
	netErr := errors.New("down")
	results := []jit.FetchResult{
		{URL: "https://acme.io/a", Err: netErr},
		{URL: "https://acme.io/b", Err: netErr},
	}
	cands, fails := jit.Emit(results, "doc")
	if cands != nil {
		t.Fatalf("candidates = %v, want nil", cands)
	}
	if len(fails) != 2 {
		t.Fatalf("failures = %d, want 2", len(fails))
	}
}

func TestEmit_EmptyURLOnSuccessSkipped(t *testing.T) {
	// Defensive: an executor shouldn't emit nil-Err+empty-URL but Emit must
	// not produce a malformed candidate if it ever does.
	results := []jit.FetchResult{
		{URL: "", Body: "x"},
		{URL: "https://acme.io/keep", Body: "ok"},
	}
	cands, fails := jit.Emit(results, "post")
	if fails != nil {
		t.Fatalf("failures = %v, want nil", fails)
	}
	if len(cands) != 1 || cands[0].URL != "https://acme.io/keep" {
		t.Fatalf("candidates = %+v, want one entry for /keep", cands)
	}
}

func TestEmit_PreviewBodySizePreserved(t *testing.T) {
	results := []jit.FetchResult{
		{URL: "https://acme.io/x", Body: "hello world"},
	}
	cands, _ := jit.Emit(results, "post")
	if len(cands) != 1 {
		t.Fatalf("candidates = %d, want 1", len(cands))
	}
	if got := cands[0].Preview["body_size"]; got != 11 {
		t.Errorf("body_size = %v, want 11", got)
	}
}

// Sanity: emitted Candidates compose cleanly with lateral.Candidate (no field
// drift).
func TestEmit_TypesCompose(t *testing.T) {
	results := []jit.FetchResult{{URL: "https://acme.io/x", Body: "y"}}
	cands, _ := jit.Emit(results, "post")
	var _ []lateral.Candidate = cands
}

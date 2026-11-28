package rederive

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/upgrade"
)

// afterTheFetchErr is a step failing on what the network returned (a
// response re-typed to a format the next step cannot read), not a fetch.
func afterTheFetchErr(id string) error {
	return fmt.Errorf("reanalyze: %q: %w", id, &pipeline.StepError{Step: "pdf_extractor", Err: errors.New("not a PDF")})
}

// In a network-mode group, a failure after the fetch is not proved by a
// success in a stored-content group: it needs evidence that the network
// works.
func TestEvidence_AFailureAfterTheFetchNeedsTheNetwork(t *testing.T) {
	t.Run("a stored-content success elsewhere proves nothing", func(t *testing.T) {
		f := newFixture(t)
		networkMode(f)
		betaOld, _ := betaPipeline(f)
		f.svc.plans["beta"] = localPlan("x")
		f.svc.fetchedFor = func(id string) []string {
			if strings.HasPrefix(id, "b") {
				return nil // stored content: no fetch
			}
			return []string{"src.test"}
		}
		f.add("a-odd", "alpha@v1", alphaOld)
		f.svc.result = func(id string, _ service.ReanalyzeOptions) error {
			if id == "a-odd" {
				return afterTheFetchErr(id)
			}
			return nil
		}
		for run := 1; run <= 6; run++ {
			f.add(fmt.Sprintf("b%d", run), "beta@v1", betaOld)
			if _, err := f.run(); err != nil {
				t.Fatal(err)
			}
		}
		if got := f.skips(alphaNew); len(got) != 0 {
			t.Fatalf("skips = %v", got)
		}
	})
	t.Run("an accepted fetch in the run makes it a strike", func(t *testing.T) {
		f := newFixture(t)
		networkMode(f)
		f.add("a-odd", "alpha@v1", alphaOld)
		f.svc.result = func(id string, _ service.ReanalyzeOptions) error {
			if id == "a-odd" {
				return afterTheFetchErr(id)
			}
			return nil
		}
		for run := 1; run <= 4; run++ {
			f.add(fmt.Sprintf("a-ok%d", run), "alpha@v1", alphaOld)
			if _, err := f.run(); err != nil {
				t.Fatal(err)
			}
			if skipped := f.skips(alphaNew)["a-odd"] == storage.SkipFailed; skipped != (run == 4) {
				t.Fatalf("after run %d: skipped = %v", run, skipped)
			}
		}
	})
}

// A rate limit never strikes, whatever else the host served in the run.
func TestEvidence_ARateLimitIsAlwaysOnlyATry(t *testing.T) {
	f := newFixture(t)
	networkMode(f)
	f.add("slow", "alpha@v1", alphaOld)
	f.svc.result = func(id string, _ service.ReanalyzeOptions) error {
		if id == "slow" {
			return fmt.Errorf("reanalyze: %w", &pipeline.StepError{Step: "url_fetcher",
				Err: &pipeline.FetchError{Host: "src.test", Status: 429, RateLimited: true, Err: errors.New("unexpected status 429")}})
		}
		return nil
	}
	for run := 1; run <= 8; run++ {
		f.add(fmt.Sprintf("ok%d", run), "alpha@v1", alphaOld) // the same host succeeds every run
		if _, err := f.run(); err != nil {
			t.Fatal(err)
		}
	}
	if got := f.skips(alphaNew); len(got) != 0 {
		t.Fatalf("skips = %v", got)
	}
}

// Two groups of one pipeline (two stored stamps) are one entry in the
// stopped list, with the count.
func TestStopped_OneEntryPerPipeline(t *testing.T) {
	f := newFixture(t)
	older := "alpha#0000000000000000"
	if err := f.store.UpsertDerivationSignature(t.Context(), older, "alpha", []string{"a@0", "b@0"}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 6; i++ {
		f.add(fmt.Sprintf("p%d", i), "alpha@v1", alphaOld)
		f.add(fmt.Sprintf("q%d", i), "alpha@v1", older)
	}
	f.svc.result = func(string, service.ReanalyzeOptions) error { return fetchErr("dial tcp: i/o timeout") }
	res, err := f.run()
	if err != nil || len(res.Stopped) != 1 || res.Stopped[0].Groups != 2 || res.Stopped[0].Pipeline != "alpha" {
		t.Fatalf("stopped = %+v, %v", res.Stopped, err)
	}
}

// The previous run's stopped groups are not shown while later groups run.
func TestStopped_ClearedWhenANewRunStarts(t *testing.T) {
	f := newFixture(t)
	prog := &fakeProgress{stopped: []upgrade.Stopped{{Pipeline: "old", Kind: "fetch", Cause: "x"}}}
	f.job.Progress = prog
	f.add("o1", "alpha@v1", alphaOld)
	seen := -1
	f.svc.onCall = func(string) {
		prog.mu.Lock()
		seen = len(prog.stopped)
		prog.mu.Unlock()
	}
	if _, err := f.run(); err != nil {
		t.Fatal(err)
	}
	if seen != 0 {
		t.Fatalf("stopped list during the run = %d entries, want it cleared at the start", seen)
	}
}

// A 403 that says it is a rate limit is one: never a strike, whatever the
// status, whatever else the host served in the run.
func TestEvidence_ARateLimitedForbiddenIsAlwaysOnlyATry(t *testing.T) {
	f := newFixture(t)
	networkMode(f)
	f.add("slow", "alpha@v1", alphaOld)
	f.svc.result = func(id string, _ service.ReanalyzeOptions) error {
		if id == "slow" {
			return fmt.Errorf("reanalyze: %w", &pipeline.StepError{Step: "url_fetcher",
				Err: &pipeline.FetchError{Host: "src.test", Status: 403, RateLimited: true, Err: errors.New("rate limit exceeded")}})
		}
		return nil
	}
	for run := 1; run <= 6; run++ {
		f.add(fmt.Sprintf("ok%d", run), "alpha@v1", alphaOld) // the same host succeeds every run
		if _, err := f.run(); err != nil {
			t.Fatal(err)
		}
	}
	if got := f.skips(alphaNew); len(got) != 0 {
		t.Fatalf("skips = %v", got)
	}
}

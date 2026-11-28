package rederive

import (
	"fmt"
	"slices"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/upgrade"
)

func addN(f *fixture, n int) []string {
	var ids []string
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("o%d", i)
		f.add(id, "alpha@v1", alphaOld)
		ids = append(ids, id)
	}
	return ids
}

func attemptsOf(t *testing.T, f *fixture, id string) int {
	t.Helper()
	got, err := f.store.ListRederivable(t.Context(), storage.RederiveQuery{
		Pipeline: "alpha@v1", Sig: alphaOld, Target: alphaNew, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range got {
		if o.ID == id {
			return o.Tries
		}
	}
	return -1
}

// Five objects that always time out must not hold the run for ever: they
// get an attempt each and stop the run, and the next run goes to the ones
// never tried first.
func TestAttempts_NeverTriedObjectsComeFirst(t *testing.T) {
	f := newFixture(t)
	addN(f, 10)
	bad := map[string]bool{"o0": true, "o1": true, "o2": true, "o3": true, "o4": true}
	f.svc.result = func(id string, _ service.ReanalyzeOptions) error {
		if bad[id] {
			return fetchErr("dial tcp: i/o timeout")
		}
		return nil
	}
	res, err := f.run()
	if err != nil || !stoppedKind(res, "fetch") || res.Processed != 0 || res.Transient != 5 {
		t.Fatalf("first run = %+v, %v", res, err)
	}
	for id := range bad {
		if a := attemptsOf(t, f, id); a != 1 {
			t.Fatalf("%s attempts = %d, want 1", id, a)
		}
	}
	f.svc.mu.Lock()
	f.svc.calls = nil
	f.svc.mu.Unlock()
	res, err = f.run()
	if err != nil || !stoppedKind(res, "fetch") || res.Processed != 5 {
		t.Fatalf("second run = %+v, %v: the healthy five must go first", res, err)
	}
	if first := f.svc.called()[:5]; !slices.Equal(first, []string{"o5", "o6", "o7", "o8", "o9"}) {
		t.Fatalf("second run began with %v", first)
	}
}

// A refusal comes between provider failures: it records only itself and
// neither resets the streak nor turns the held failures into skips.
func TestAttempts_ARefusalBetweenProviderFailures(t *testing.T) {
	f := newFixture(t)
	addN(f, 6)
	f.svc.result = func(id string, _ service.ReanalyzeOptions) error {
		if id == "o2" {
			return fmt.Errorf("x: %w", upgrade.ErrCannotReanalyze)
		}
		return llmFailure(id)
	}
	res, err := f.run()
	if err != nil || !stoppedKind(res, "llm") {
		t.Fatalf("run = %+v, %v", res, err)
	}
	got := f.skips(alphaNew)
	if len(got) != 1 || got["o2"] != storage.SkipRefused {
		t.Fatalf("skips = %v, want only the refusal", got)
	}
	for _, id := range []string{"o0", "o1", "o3", "o4", "o5"} {
		if a := attemptsOf(t, f, id); a != 1 {
			t.Errorf("%s attempts = %d", id, a)
		}
	}
}

// Ollama not up yet at boot: fewer than five objects, all failing. One
// attempt each, nothing recorded as a skip, all still stale.
func TestAttempts_ProviderDownWithFewObjectsRecordsNoSkip(t *testing.T) {
	f := newFixture(t)
	addN(f, 3)
	f.svc.result = func(id string, _ service.ReanalyzeOptions) error { return llmFailure(id) }
	res, err := f.run()
	if err != nil || res.Transient != 3 || res.SkippedTotal() != 0 {
		t.Fatalf("run = %+v, %v", res, err)
	}
	if len(f.skips(alphaNew)) != 0 || f.stale() != 3 {
		t.Fatalf("skips %v stale %d", f.skips(alphaNew), f.stale())
	}
	f.svc.result = nil // Ollama is up
	res, err = f.run()
	if err != nil || res.Processed != 3 {
		t.Fatalf("next start = %+v, %v", res, err)
	}
}

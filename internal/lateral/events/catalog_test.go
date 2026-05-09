package events

import (
	"testing"

	"hop.top/kit/go/runtime/bus"
	"hop.top/kit/go/runtime/notify"
)

func TestCatalog_AllTopicsNonEmpty(t *testing.T) {
	want := []bus.Topic{
		ScanStarted, ScanCompleted, ScanDeferred, ScanSkipped, ScanFailed,
		SubpathFailed,
		CandidateCreated, CandidatePromoted, CandidateExpired, CandidateRejected, CandidateResurrected,
		ReaperCycleStarted, ReaperCycleCompleted, ReaperCycleFailed, ReaperCycleSkipped,
		SanityCheckViolated,
		ScoreFlagged, SignalDegraded, ResolutionFlagged,
		RecipeRefreshed, RecipeServed,
		ClassificationMatched, ClassificationClassified, ClassificationCached,
		ResearchIntentBoosted, ResearchIntentSuppressed,
	}
	if len(want) != 26 {
		t.Fatalf("expected 26 topic constants in want list, got %d", len(want))
	}
	for _, topic := range want {
		if topic == "" {
			t.Fatal("topic must not be empty")
		}
	}
}

func TestCatalog_TopicsMatchSchema(t *testing.T) {
	// Smoke check: a few known wire forms.
	cases := map[bus.Topic]string{
		ScanStarted:           "ctxt.lateral.scan.started",
		ReaperCycleCompleted:  "ctxt.lateral.reaper_cycle.completed",
		SanityCheckViolated:   "ctxt.lateral.sanity_check.violated",
		ResearchIntentBoosted: "ctxt.lateral.research_intent.boosted",
		ScoreFlagged:          "ctxt.lateral.score.flagged",
	}
	for topic, want := range cases {
		if string(topic) != want {
			t.Errorf("topic = %q, want %q", string(topic), want)
		}
	}
}

func TestPayload_SeverityMapping(t *testing.T) {
	cases := []struct {
		name    string
		payload notify.WithSeverity
		want    notify.Severity
	}{
		{"failure", FailurePayload{Topic: SubpathFailed}, notify.SeverityError},
		{"sanity", SanityViolationPayload{Topic: SanityCheckViolated}, notify.SeverityCritical},
		{"info", CyclePayload{Topic: ReaperCycleCompleted}, notify.SeverityInfo},
		{"lifecycle", CandidateLifecyclePayload{Topic: CandidateCreated}, notify.SeverityInfo},
	}
	for _, c := range cases {
		if got := c.payload.Severity(); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

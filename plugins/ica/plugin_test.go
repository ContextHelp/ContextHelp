package ica_test

import (
	"net/http"
	"testing"

	ica "github.com/ideacrafterslabs/ctxt/plugins/ica"
	icaSteps "github.com/ideacrafterslabs/ctxt/plugins/ica/steps"
)

func newTestPlugin(cfg ica.Config) *ica.Plugin {
	p := ica.NewWithSteps(icaSteps.Factory())
	// Inject config + client via exported Init helper or
	// set fields through the exported constructor path.
	// Since Init probes health endpoints (undesirable in
	// tests), use InitForTest.
	p.InitForTest(cfg, &http.Client{})
	return p
}

func TestPipelineStepsWithFullConfig(t *testing.T) {
	p := newTestPlugin(ica.Config{
		APIURL:       "http://api.test",
		ProcessorURL: "http://proc.test",
	})

	steps := p.PipelineSteps()
	if len(steps) != 4 {
		t.Fatalf("want 4 steps, got %d", len(steps))
	}

	want := []string{
		"ica_feed_manager",
		"ica_fetcher",
		"ica_normalizer",
		"ica_processor",
	}
	for i, name := range want {
		if steps[i].Name() != name {
			t.Errorf(
				"step[%d]: want %q, got %q",
				i, name, steps[i].Name(),
			)
		}
	}
}

func TestPipelineStepsNoProcessor(t *testing.T) {
	p := newTestPlugin(ica.Config{
		APIURL: "http://api.test",
	})

	steps := p.PipelineSteps()
	if len(steps) != 3 {
		t.Fatalf("want 3 steps, got %d", len(steps))
	}

	want := []string{
		"ica_feed_manager",
		"ica_fetcher",
		"ica_normalizer",
	}
	for i, name := range want {
		if steps[i].Name() != name {
			t.Errorf(
				"step[%d]: want %q, got %q",
				i, name, steps[i].Name(),
			)
		}
	}
}

func TestPipelineStepsNoAPI(t *testing.T) {
	p := newTestPlugin(ica.Config{})

	steps := p.PipelineSteps()
	if len(steps) != 1 {
		t.Fatalf("want 1 step, got %d", len(steps))
	}
	if steps[0].Name() != "ica_normalizer" {
		t.Errorf(
			"want ica_normalizer, got %q",
			steps[0].Name(),
		)
	}
}

func TestPipelineDefStepOrder(t *testing.T) {
	p := newTestPlugin(ica.Config{})

	defs := p.PipelineDefs()
	if len(defs) != 1 {
		t.Fatalf("want 1 pipeline def, got %d", len(defs))
	}

	d := defs[0]
	if d.Name != "ica.feed_sync" {
		t.Errorf("want name ica.feed_sync, got %q", d.Name)
	}

	wantSteps := []string{
		"ica_feed_manager",
		"ica_fetcher",
		"ica_normalizer",
		"item_deduplicator",
		"ica_processor",
		"alternative_detector",
		"item_enqueuer",
	}
	if len(d.Steps) != len(wantSteps) {
		t.Fatalf(
			"want %d steps, got %d",
			len(wantSteps), len(d.Steps),
		)
	}
	for i, name := range wantSteps {
		if d.Steps[i] != name {
			t.Errorf(
				"step[%d]: want %q, got %q",
				i, name, d.Steps[i],
			)
		}
	}

	wantProviders := []string{"ica_api", "ica_processor"}
	if len(d.Providers) != len(wantProviders) {
		t.Fatalf(
			"want %d providers, got %d",
			len(wantProviders), len(d.Providers),
		)
	}
	for i, name := range wantProviders {
		if d.Providers[i] != name {
			t.Errorf(
				"provider[%d]: want %q, got %q",
				i, name, d.Providers[i],
			)
		}
	}
}

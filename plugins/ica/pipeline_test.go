package ica

import (
	"context"
	"net/http"
	"testing"

	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
)

// stubStep is a minimal PipelineStep for testing.
type stubStep struct{ name string }

func (s stubStep) Name() string { return s.name }

func (s stubStep) Contract() pluginapi.StepContract {
	return pluginapi.StepContract{}
}

func (s stubStep) Run(
	_ context.Context, o *pluginapi.KnowledgeObject,
) (*pluginapi.KnowledgeObject, error) {
	return o, nil
}

// fullPlugin returns a Plugin wired with stub step factories and
// both URLs configured so PipelineSteps() returns all steps.
func fullPlugin() *Plugin {
	p := NewWithSteps(StepFactory{
		NewFeedManager: func(
			_ string, _ *http.Client,
		) pluginapi.PipelineStep {
			return stubStep{name: "ica_feed_manager"}
		},
		NewFetcher: func(
			_ string, _ *http.Client,
		) pluginapi.PipelineStep {
			return stubStep{name: "ica_fetcher"}
		},
		NewNormalizer: func(
			_ bool,
		) pluginapi.PipelineStep {
			return stubStep{name: "ica_normalizer"}
		},
		NewProcessor: func(
			_ string, _ *http.Client,
		) pluginapi.PipelineStep {
			return stubStep{name: "ica_processor"}
		},
	})
	p.InitForTest(Config{
		APIURL:       "http://localhost:9999",
		ProcessorURL: "http://localhost:9998",
	}, &http.Client{})
	return p
}

func TestICAFeedSyncRegistered(t *testing.T) {
	defs := fullPlugin().PipelineDefs()
	if len(defs) != 1 {
		t.Fatalf("expected 1 pipeline def, got %d", len(defs))
	}
	if defs[0].Name != "ica.feed_sync" {
		t.Errorf(
			"name: got %q, want %q",
			defs[0].Name, "ica.feed_sync",
		)
	}
}

func TestICAFeedSyncStepsMatchDef(t *testing.T) {
	def := fullPlugin().PipelineDefs()[0]
	want := []string{
		"ica_feed_manager",
		"ica_fetcher",
		"ica_normalizer",
		"item_deduplicator",
		"ica_processor",
		"alternative_detector",
		"item_enqueuer",
	}
	if len(def.Steps) != len(want) {
		t.Fatalf(
			"step count: got %d, want %d",
			len(def.Steps), len(want),
		)
	}
	for i, s := range want {
		if def.Steps[i] != s {
			t.Errorf(
				"step[%d]: got %q, want %q",
				i, def.Steps[i], s,
			)
		}
	}
}

func TestICAFeedSyncDescription(t *testing.T) {
	def := fullPlugin().PipelineDefs()[0]
	if def.Description == "" {
		t.Error("pipeline def has empty description")
	}
}

func TestICAFeedSyncProviders(t *testing.T) {
	def := fullPlugin().PipelineDefs()[0]
	want := map[string]bool{
		"ica_api":       true,
		"ica_processor": true,
	}
	got := make(map[string]bool, len(def.Providers))
	for _, p := range def.Providers {
		got[p] = true
	}
	for name := range want {
		if !got[name] {
			t.Errorf("missing provider %q", name)
		}
	}
	if len(def.Providers) != len(want) {
		t.Errorf(
			"provider count: got %d, want %d",
			len(def.Providers), len(want),
		)
	}
}

func TestICAPluginStepNamesUnique(t *testing.T) {
	steps := fullPlugin().PipelineSteps()
	seen := make(map[string]bool, len(steps))
	for _, s := range steps {
		name := s.Name()
		if seen[name] {
			t.Errorf("duplicate step name %q", name)
		}
		seen[name] = true
	}
}

func TestICAPluginStepNamesMatchDef(t *testing.T) {
	p := fullPlugin()
	def := p.PipelineDefs()[0]

	defSteps := make(map[string]bool, len(def.Steps))
	for _, s := range def.Steps {
		defSteps[s] = true
	}

	for _, step := range p.PipelineSteps() {
		name := step.Name()
		if !defSteps[name] {
			t.Errorf(
				"step %q not in pipeline def steps",
				name,
			)
		}
	}
}

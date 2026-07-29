package steps

import (
	"net/http"
	"testing"

	ica "github.com/ideacrafterslabs/ctxt/plugins/ica"
)

func newDegradationPlugin(cfg ica.Config) *ica.Plugin {
	p := ica.NewWithSteps(Factory())
	p.InitForTest(cfg, &http.Client{})
	return p
}

func TestICAFeedSyncCapabilityDegradation_NoProcessor(t *testing.T) {
	p := newDegradationPlugin(ica.Config{
		APIURL: "http://api.test",
	})

	steps := p.PipelineSteps()
	if len(steps) != 3 {
		t.Fatalf("want 3 steps, got %d", len(steps))
	}

	// PipelineDef still lists all 7 step names.
	defs := p.PipelineDefs()
	if len(defs) != 1 {
		t.Fatalf("want 1 pipeline def, got %d", len(defs))
	}
	if len(defs[0].Steps) != 7 {
		t.Fatalf(
			"def should list all 7 steps, got %d",
			len(defs[0].Steps),
		)
	}

	// Verify step ordering and contract chaining:
	// feed_manager (produces Metadata) ->
	// fetcher (requires Source, produces RawContent+Metadata) ->
	// normalizer (requires Metadata, produces Metadata+Mentions+
	//   Sections+Tags)
	wantNames := []string{
		"ica_feed_manager", "ica_fetcher", "ica_normalizer",
	}
	for i, name := range wantNames {
		if steps[i].Name() != name {
			t.Errorf(
				"step[%d]: want %q, got %q",
				i, name, steps[i].Name(),
			)
		}
	}

	// Contracts chain: each step's produces feed next's requires.
	c0 := steps[0].Contract()
	assertSlice(t, "step[0].Produces", c0.Produces, []string{
		"Metadata",
	})

	c1 := steps[1].Contract()
	assertSlice(t, "step[1].Requires", c1.Requires, []string{
		"Source",
	})
	assertSlice(t, "step[1].Produces", c1.Produces, []string{
		"RawContent", "Metadata",
	})

	c2 := steps[2].Contract()
	assertSlice(t, "step[2].Requires", c2.Requires, []string{
		"Metadata",
	})
	assertSlice(t, "step[2].Produces", c2.Produces, []string{
		"Metadata", "Mentions", "Sections", "Tags",
	})
}

func TestICAFeedSyncCapabilityDegradation_NoAPI(t *testing.T) {
	p := newDegradationPlugin(ica.Config{})

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

	// Normalizer is self-sufficient: requires Metadata (from seed
	// or previous pipeline), no capabilities needed.
	c := steps[0].Contract()
	assertSlice(t, "Requires", c.Requires, []string{
		"Metadata",
	})
	if len(c.Capabilities) != 0 {
		t.Errorf(
			"normalizer should need no capabilities, got %v",
			c.Capabilities,
		)
	}
}

func TestICAFeedSyncCapabilityDegradation_FullCapabilities(
	t *testing.T,
) {
	p := newDegradationPlugin(ica.Config{
		APIURL:       "http://api.test",
		ProcessorURL: "http://proc.test",
	})

	steps := p.PipelineSteps()
	if len(steps) != 4 {
		t.Fatalf("want 4 steps, got %d", len(steps))
	}

	// All 4 steps must have valid contracts (non-nil Produces).
	for i, s := range steps {
		c := s.Contract()
		if len(c.Produces) == 0 {
			t.Errorf(
				"step[%d] (%s): empty Produces",
				i, s.Name(),
			)
		}
	}

	wantNames := []string{
		"ica_feed_manager",
		"ica_fetcher",
		"ica_normalizer",
		"ica_processor",
	}
	for i, name := range wantNames {
		if steps[i].Name() != name {
			t.Errorf(
				"step[%d]: want %q, got %q",
				i, name, steps[i].Name(),
			)
		}
	}
}

func TestICAFeedSyncStepContracts(t *testing.T) {
	p := newDegradationPlugin(ica.Config{
		APIURL:       "http://api.test",
		ProcessorURL: "http://proc.test",
	})

	steps := p.PipelineSteps()
	if len(steps) != 4 {
		t.Fatalf("want 4 steps, got %d", len(steps))
	}

	tests := []struct {
		name     string
		requires []string
		produces []string
		caps     []string
	}{
		{
			name:     "ica_feed_manager",
			requires: []string{"Source"},
			produces: []string{"Metadata"},
			caps:     []string{"ica_api"},
		},
		{
			name:     "ica_fetcher",
			requires: []string{"Source"},
			produces: []string{"RawContent", "Metadata"},
			caps:     []string{"ica_api"},
		},
		{
			name:     "ica_normalizer",
			requires: []string{"Metadata"},
			produces: []string{
				"Metadata", "Mentions", "Sections", "Tags",
			},
			caps: []string{},
		},
		{
			name:     "ica_processor",
			requires: []string{"RawContent"},
			produces: []string{
				"Embeddings", "Metadata", "Sections",
			},
			caps: []string{"ica_processor"},
		},
	}

	for i, tt := range tests {
		s := steps[i]
		if s.Name() != tt.name {
			t.Errorf(
				"step[%d]: want name %q, got %q",
				i, tt.name, s.Name(),
			)
			continue
		}
		c := s.Contract()
		assertSlice(
			t, tt.name+".Requires", c.Requires, tt.requires,
		)
		assertSlice(
			t, tt.name+".Produces", c.Produces, tt.produces,
		)
		assertSlice(
			t, tt.name+".Capabilities",
			c.Capabilities, tt.caps,
		)
	}
}

// assertSlice compares two string slices by position.
func assertSlice(
	t *testing.T, label string,
	got, want []string,
) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf(
			"%s: len %d, want %d (%v vs %v)",
			label, len(got), len(want), got, want,
		)
		return
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf(
				"%s[%d]: got %q, want %q",
				label, i, got[i], want[i],
			)
		}
	}
}

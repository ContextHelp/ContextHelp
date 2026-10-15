package builtins

import (
	"slices"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/sharing"
)

func TestInjectClassRulesStep(t *testing.T) {
	for _, tc := range []struct {
		name   string
		steps  []string
		access string
		want   []string
	}{
		{"private skips", []string{"typedetector", "structured_metadata", "embedding"}, config.AccessPrivate, []string{"typedetector", "structured_metadata", "embedding"}},
		{"unset skips", []string{"typedetector", "embedding"}, "", []string{"typedetector", "embedding"}},
		{"after structured_metadata", []string{"typedetector", "structured_metadata", "c12n_classify", "embedding"}, config.AccessProtected, []string{"typedetector", "structured_metadata", "content_classes", "c12n_classify", "embedding"}},
		{"before embedding", []string{"typedetector", "tagger", "embedding", "dedup"}, config.AccessPublic, []string{"typedetector", "tagger", "content_classes", "embedding", "dedup"}},
		{"last", []string{"feed_fetcher", "feed_parser"}, config.AccessProtected, []string{"feed_fetcher", "feed_parser", "content_classes"}},
		{"already present", []string{"content_classes", "embedding"}, config.AccessProtected, []string{"content_classes", "embedding"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := InjectClassRulesStep(tc.steps, tc.access); !slices.Equal(got, tc.want) {
				t.Fatalf("InjectClassRulesStep = %v, want %v", got, tc.want)
			}
		})
	}
}

// A registry built for a sharing instance runs content_classes in the
// text pipelines, after the extraction steps and before the embedding
// (structured_metadata itself is pruned without an LLM provider, so
// the step follows entity_resolver here); a private one does not.
func TestRegistry_ContentClassesOnSharingInstances(t *testing.T) {
	for _, tc := range []struct {
		access string
		want   bool
	}{{config.AccessPrivate, false}, {config.AccessProtected, true}, {config.AccessPublic, true}} {
		t.Run(tc.access, func(t *testing.T) {
			reg := ConfiguredRegistryWithOpts(BuildOpts{Access: tc.access, ClassRules: sharing.DefaultClassRules()})
			for _, name := range []string{"text.short", "text.long"} {
				names := stepNames(t, reg, name)
				i := slices.Index(names, "content_classes")
				if (i >= 0) != tc.want {
					t.Fatalf("%s steps = %v, want content_classes present = %v", name, names, tc.want)
				}
				if !tc.want {
					continue
				}
				if j := slices.Index(names, "entity_resolver"); j < 0 || j > i {
					t.Errorf("%s steps = %v: content_classes must follow entity_resolver", name, names)
				}
				if j := slices.Index(names, "embedding_generator"); j >= 0 && j < i {
					t.Errorf("%s steps = %v: content_classes must precede the embedding", name, names)
				}
			}
		})
	}
	if !IsStepName("content_classes") {
		t.Error("content_classes is not a built-in step name")
	}
}

func TestClassRulesFromConfig(t *testing.T) {
	got := ClassRulesFromConfig(config.SharingConfig{})
	if !slices.Equal(got, sharing.DefaultClassRules()) {
		t.Errorf("empty config = %+v, want the defaults", got)
	}
	cfg := config.SharingConfig{Classes: config.ClassesConfig{Rules: []config.ClassRuleConfig{{Class: "financial", Tag: "invoices", Pipeline: "text.long"}}}}
	got = ClassRulesFromConfig(cfg)
	if len(got) != 3 || got[2] != (sharing.ClassRule{Class: "financial", Tag: "invoices", Pipeline: "text.long"}) {
		t.Errorf("configured = %+v, want defaults then the rule", got)
	}
	cfg.Classes.NoDefaults = true
	if got = ClassRulesFromConfig(cfg); len(got) != 1 || got[0].Class != "financial" {
		t.Errorf("no_defaults = %+v, want the rule only", got)
	}
	if SharingEnabled(config.AccessPrivate) || SharingEnabled("") || !SharingEnabled(config.AccessProtected) || !SharingEnabled(config.AccessPublic) {
		t.Error("SharingEnabled")
	}
}

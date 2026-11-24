package config

import (
	"math"
	"strings"
	"testing"
)

func TestValidateFramesRole(t *testing.T) {
	bad := map[string]ProviderBackendConfig{
		"providers.frames.backend":         {Backend: "magic"},
		"providers.frames.min_fps":         {MinFPS: math.NaN()},
		"providers.frames.max_fps":         {MaxFPS: math.Inf(1)},
		"providers.frames.scene_threshold": {SceneThreshold: 1.5},
		"providers.frames.max_frames":      {MaxFrames: -1},
		"providers.frames.max_dimension":   {MaxDimension: 99_999},
	}
	for field, c := range bad {
		errs := validateFramesRole(c, "providers.frames")
		if len(errs) == 0 || errs[0].Field != field {
			t.Errorf("%s: got %v", field, errs)
		}
	}
	// Equal rates leave no valid spacing: ceil(1000/0.3) > floor(1000/0.3).
	if errs := validateFramesRole(ProviderBackendConfig{MinFPS: 0.3, MaxFPS: 0.3}, "providers.frames"); len(errs) != 1 || !strings.Contains(errs[0].Message, "no valid spacing") {
		t.Errorf("equal rates: %v", errs)
	}
	if errs := validateFramesRole(ProviderBackendConfig{MinFPS: 2, MaxFPS: 1}, "providers.frames"); len(errs) != 1 {
		t.Errorf("min above max: %v", errs)
	}
	if errs := validateFramesRole(ProviderBackendConfig{MaxDimension: -2}, "providers.frames"); len(errs) != 1 {
		t.Errorf("max_dimension -2: %v", errs)
	}
	for _, ok := range []ProviderBackendConfig{{MaxDimension: FrameNoScaling}, {}, {Backend: "ffmpeg", MinFPS: 0.1, MaxFPS: 1, SceneThreshold: 0.3, MaxFrames: 10_000}, {MinFPS: 0.5, MaxFPS: 0.5}} {
		if errs := validateFramesRole(ok, "providers.frames"); len(errs) != 0 {
			t.Errorf("%+v: %v", ok, errs)
		}
	}
}

func TestValidateSummarizationRole(t *testing.T) {
	if errs := validateSummarizationRole(ProviderBackendConfig{Backend: "gpt-sorcery"}, "providers.summarization"); len(errs) != 1 || errs[0].Field != "providers.summarization.backend" {
		t.Errorf("unknown backend: %v", errs)
	}
	if errs := validateSummarizationRole(ProviderBackendConfig{NumCtx: -1}, "providers.summarization"); len(errs) != 1 {
		t.Errorf("negative num_ctx: %v", errs)
	}
	for _, b := range []string{"", "auto", "ollama", "openai", "anthropic", "stub"} {
		if errs := validateSummarizationRole(ProviderBackendConfig{Backend: b}, "x"); len(errs) != 0 {
			t.Errorf("%q: %v", b, errs)
		}
	}
}

func TestValidateChecksProviderRolesAndOverrides(t *testing.T) {
	c := &Config{}
	c.Providers.Frames = ProviderBackendConfig{MaxFPS: 5000}
	c.Pipelines.Overrides = map[string]PipelineOverride{
		"video.full": {Providers: map[string]ProviderBackendConfig{"summarization": {Backend: "nope"}}},
	}
	got := map[string]bool{}
	for _, e := range Validate(c) {
		got[e.Field] = true
	}
	for _, f := range []string{"providers.frames.max_fps", "pipelines.overrides.video.full.providers.summarization.backend"} {
		if !got[f] {
			t.Errorf("no error for %s; got %v", f, got)
		}
	}
}

func TestValidateSummarizationTemplateAtLoad(t *testing.T) {
	for _, bad := range []string{"{{.Text", "no text", "{{if .Language}}{{.Text}}{{end}}", "{{.Text}}{{.Missing}}"} {
		errs := validateSummarizationRole(ProviderBackendConfig{PromptTemplate: bad}, "providers.summarization")
		if len(errs) != 1 || errs[0].Field != "providers.summarization.prompt_template" {
			t.Errorf("%q: %v", bad, errs)
		}
	}
	if errs := validateSummarizationRole(ProviderBackendConfig{PromptTemplate: "Summarize in {{.Language}}: {{.Text}}"}, "x"); len(errs) != 0 {
		t.Errorf("good template: %v", errs)
	}
}

func TestFrameGapsMS(t *testing.T) {
	if lo, hi := FrameGapsMS(0.6, 1); lo != 1000 || hi != 1666 {
		t.Errorf("0.6/1: %d, %d", lo, hi)
	}
	if lo, hi := FrameGapsMS(0, 0); lo != 0 || hi != 0 {
		t.Errorf("unset: %d, %d", lo, hi)
	}
}

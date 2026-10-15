package builtins_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline/builtins"
	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// fakeExternal resolves installed steps by name, recording each insert
// it built a step for.
type fakeExternal struct {
	installed map[string]bool
	built     []config.StepInsert
}

func (f *fakeExternal) Has(name string) bool { return f.installed[name] }

func (f *fakeExternal) Step(_ string, ins config.StepInsert) (pipeline.PipelineStep, error) {
	f.built = append(f.built, ins)
	return &namedStep{name: ins.Step}, nil
}

type namedStep struct {
	pipeline.BaseContract
	name string
}

func (s *namedStep) Name() string { return s.name }
func (s *namedStep) Run(_ context.Context, d *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	return d, nil
}

func stepNames(p *pipeline.Pipeline) []string {
	out := make([]string, len(p.Steps))
	for i, s := range p.Steps {
		out[i] = s.Name()
	}
	return out
}

func buildWith(t *testing.T, ext builtins.ExternalSteps, o map[string]config.PipelineOverride) (pipeline.Registry, error) {
	t.Helper()
	baseCfg := config.ProvidersConfig{}
	opts := builtins.BuildOpts{Factory: providers.NewFactory(baseCfg, nil), External: ext}
	return builtins.ConfiguredRegistryWithPipelineOverrides(opts, baseCfg, config.PipelinesConfig{Overrides: o})
}

func TestPipelineStructuralOverride(t *testing.T) {
	reg, err := buildWith(t, nil, map[string]config.PipelineOverride{
		"text.short": {
			SkipSteps:   []string{"tagger"},
			InsertSteps: []config.StepInsert{{Step: "noop"}},
		},
	})
	require.NoError(t, err)
	pipe, err := reg.Get("text.short")
	require.NoError(t, err)

	names := stepNames(pipe)
	assert.NotContains(t, names, "tagger", "tagger should have been skipped")
	require.NotEmpty(t, names)
	assert.Equal(t, "noop", names[len(names)-1], "an insert without an anchor appends")
}

// TestInsertStepsPlacement: before/after put the step right next to its
// anchor, inserts apply in order and may anchor on an earlier insert.
func TestInsertStepsPlacement(t *testing.T) {
	ext := &fakeExternal{installed: map[string]bool{"pii-mask": true}}
	reg, err := buildWith(t, ext, map[string]config.PipelineOverride{
		"text.short": {InsertSteps: []config.StepInsert{
			{Step: "pii-mask", Before: "typedetector", OnError: config.StepOnErrorFail},
			{Step: "noop", After: "pii-mask"},
			{Step: "textcleaner", After: "entity_extractor"},
		}},
	})
	require.NoError(t, err)
	pipe, err := reg.Get("text.short")
	require.NoError(t, err)

	// Runtime step names (typedetect, text_cleaner, embedding_generator)
	// differ from the names config uses for some built-ins.
	assert.Equal(t, []string{
		"pii-mask", "noop", "typedetect", "tagger", "entity_extractor", "text_cleaner",
		"entity_resolver", "structured_metadata", "c12n_classify", "embedding_generator",
	}, stepNames(pipe))
	require.Len(t, ext.built, 1, "only the installed step is built by the external resolver")
	assert.Equal(t, "text.short", pipe.PipelineName)
	assert.Equal(t, "typedetector", ext.built[0].Before)

	// Other pipelines are untouched.
	long, err := reg.Get("text.long")
	require.NoError(t, err)
	assert.NotContains(t, stepNames(long), "pii-mask")
}

func TestOverrideValidationErrors(t *testing.T) {
	ext := &fakeExternal{installed: map[string]bool{"pii-mask": true}}
	cases := []struct {
		name string
		o    config.PipelineOverride
		pipe string
		want string
	}{
		{"unknown inserted step", config.PipelineOverride{InsertSteps: []config.StepInsert{{Step: "pii-mask2", Before: "tagger"}}},
			"text.short", `insert_steps[0].step: unknown step "pii-mask2"`},
		{"unknown before anchor", config.PipelineOverride{InsertSteps: []config.StepInsert{{Step: "pii-mask", Before: "nope"}}},
			"text.short", `insert_steps[0].before: step "nope" is not in pipeline text.short`},
		{"unknown after anchor", config.PipelineOverride{InsertSteps: []config.StepInsert{{Step: "pii-mask", After: "sectioner"}}},
			"text.short", `insert_steps[0].after: step "sectioner" is not in pipeline text.short`},
		{"anchor removed by skip_steps", config.PipelineOverride{SkipSteps: []string{"tagger"}, InsertSteps: []config.StepInsert{{Step: "pii-mask", Before: "tagger"}}},
			"text.short", `insert_steps[0].before: step "tagger" is not in pipeline text.short`},
		{"unknown skip step", config.PipelineOverride{SkipSteps: []string{"taggr"}},
			"text.short", `skip_steps[0]: step "taggr" is not in pipeline text.short`},
		{"unknown pipeline", config.PipelineOverride{SkipSteps: []string{"tagger"}},
			"text.shrot", `pipelines.overrides.text.shrot: unknown pipeline "text.shrot"`},
		{"both anchors", config.PipelineOverride{InsertSteps: []config.StepInsert{{Step: "pii-mask", Before: "tagger", After: "tagger"}}},
			"text.short", `set before or after, not both`},
		{"run options on a built-in step", config.PipelineOverride{InsertSteps: []config.StepInsert{{Step: "noop", OnError: config.StepOnErrorContinue}}},
			"text.short", `insert_steps[0]: on_error, timeout, isolation and config apply to installed steps only; "noop" is built in`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var reg pipeline.Registry
			var err error
			require.NotPanics(t, func() {
				reg, err = buildWith(t, ext, map[string]config.PipelineOverride{tc.pipe: tc.o})
			})
			require.Error(t, err)
			assert.Nil(t, reg)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

// TestValidateOverridesReportsEveryProblem: config validation lists each
// problem, sorted by field, without building anything.
func TestValidateOverridesReportsEveryProblem(t *testing.T) {
	errs := builtins.ValidateOverrides(config.PipelinesConfig{Overrides: map[string]config.PipelineOverride{
		"text.short": {
			SkipSteps:   []string{"nope"},
			InsertSteps: []config.StepInsert{{Step: "pii-mask", Before: "tagger"}},
		},
	}}, config.DuplicatesConfig{}, func(string) bool { return false })
	var msgs []string
	for _, e := range errs {
		msgs = append(msgs, e.Error())
	}
	joined := strings.Join(msgs, "\n")
	require.Len(t, errs, 2, joined)
	assert.Contains(t, joined, `pipelines.overrides.text.short.insert_steps[0].step: unknown step "pii-mask": not a built-in step and not installed`)
	assert.Contains(t, joined, `pipelines.overrides.text.short.skip_steps[0]: step "nope" is not in pipeline text.short`)

	// An installed name is known.
	errs = builtins.ValidateOverrides(config.PipelinesConfig{Overrides: map[string]config.PipelineOverride{
		"text.short": {InsertSteps: []config.StepInsert{{Step: "pii-mask", Before: "tagger"}}},
	}}, config.DuplicatesConfig{}, func(n string) bool { return n == "pii-mask" })
	assert.Empty(t, errs)
}

// TestDedupAnchor: the dedup step check_similar inserts is a valid anchor.
func TestDedupAnchor(t *testing.T) {
	errs := builtins.ValidateOverrides(config.PipelinesConfig{Overrides: map[string]config.PipelineOverride{
		"text.short": {InsertSteps: []config.StepInsert{{Step: "noop", After: "dedup"}}},
	}}, config.DuplicatesConfig{CheckSimilar: true}, nil)
	assert.Empty(t, errs)
	errs = builtins.ValidateOverrides(config.PipelinesConfig{Overrides: map[string]config.PipelineOverride{
		"text.short": {InsertSteps: []config.StepInsert{{Step: "noop", After: "dedup"}}},
	}}, config.DuplicatesConfig{}, nil)
	assert.Len(t, errs, 1)
}

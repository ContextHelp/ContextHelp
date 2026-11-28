package service

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline/builtins"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline/steps"
	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/upgrade"
)

func (e *realEnv) llmCalls() int {
	e.llm.mu.Lock()
	defer e.llm.mu.Unlock()
	return e.llm.calls
}

// useText replaces the registry the service re-analyses with by one that
// holds only p as text.long.
func (e *realEnv) useText(t *testing.T, p *pipeline.Pipeline) {
	t.Helper()
	reg := pipeline.NewRegistry()
	require.NoError(t, reg.Register(pipeline.TextLong, p))
	e.svc.Pipes = reg
}

// textLongWith is the working text.long with the step named name swapped.
func (e *realEnv) textLongWith(t *testing.T, name string, st pipeline.PipelineStep) *pipeline.Pipeline {
	t.Helper()
	orig, err := e.reg.Get(pipeline.TextLong)
	require.NoError(t, err)
	cp := *orig
	cp.Steps = slices.Clone(orig.Steps)
	i := slices.IndexFunc(cp.Steps, func(s pipeline.PipelineStep) bool { return s.Name() == name })
	require.GreaterOrEqual(t, i, 0, "text.long has no step %s", name)
	cp.Steps[i] = st
	return &cp
}

// refusedUnchanged asserts the reanalysis of id is refused with reason in
// its message, with the row, the outbox and the provider calls as they were.
func (e *realEnv) refusedUnchanged(t *testing.T, id string, reason ...string) {
	t.Helper()
	before := e.get(t, id)
	head, _ := e.svc.Outbox().Head(ownerCtx())
	calls := e.llmCalls()

	_, _, err := e.svc.ReanalyzeObject(ownerCtx(), id)
	require.ErrorIs(t, err, upgrade.ErrCannotReanalyze)
	for _, r := range reason {
		assert.Contains(t, err.Error(), r)
	}
	assert.Equal(t, before, e.get(t, id), "the row changed")
	head2, _ := e.svc.Outbox().Head(ownerCtx())
	assert.Equal(t, head, head2, "an event was emitted")
	assert.Equal(t, calls, e.llmCalls(), "a provider was called")
}

func enrichedObject(t *testing.T, e *realEnv) string {
	t.Helper()
	first := e.ingest(t, "text.long", "test", llmDoc)
	require.Contains(t, tagSources(first.Tags), "llmtagone", "fixture: llm tags")
	require.NotNil(t, first.Sections[0].Metadata["summary"], "fixture: llm section summary")
	require.True(t, hasMention(first.Mentions, "@person.ada"), "fixture: llm mention")
	return first.ID
}

// A pipeline rebuilt without its LLM is refused, one case per way the
// provider can be missing.
func TestReanalyzeCapability_RefusedWithoutLLM(t *testing.T) {
	t.Run("graph_extractor pruned", func(t *testing.T) {
		e := newRealEnv(t)
		id := enrichedObject(t, e)
		orig, err := e.reg.Get(pipeline.TextLong)
		require.NoError(t, err)
		cp := *orig
		cp.Pruned = []pipeline.PrunedStep{{Name: "graph_extractor", Resources: []string{pipeline.ResourceLLM}}}
		e.useText(t, &cp)
		e.refusedUnchanged(t, id, "llm provider", "step graph_extractor")
	})
	t.Run("nil provider", func(t *testing.T) {
		e := newRealEnv(t)
		id := enrichedObject(t, e)
		e.useText(t, e.textLongWith(t, "sectioner", steps.NewSectioner()))
		e.refusedUnchanged(t, id, "llm provider", "step sectioner")
	})
	t.Run("stub backend", func(t *testing.T) {
		e := newRealEnv(t)
		id := enrichedObject(t, e)
		e.useText(t, e.textLongWith(t, "tagger", steps.NewTaggerWithLLM(providers.NewStubLLMProvider())))
		e.refusedUnchanged(t, id, "llm provider", "step tagger", "stub")
	})
	t.Run("registry rebuilt with the stub llm", func(t *testing.T) {
		e := newRealEnv(t)
		id := enrichedObject(t, e)
		f := providers.NewFactory(config.ProvidersConfig{LLM: config.ProviderBackendConfig{Backend: config.BackendStub}}, nil)
		reg, err := builtins.ConfiguredRegistryWithOpts(builtins.BuildOpts{Factory: f})
		require.NoError(t, err)
		e.svc.Pipes = reg
		e.refusedUnchanged(t, id, "llm provider")
	})
	t.Run("network allowed", func(t *testing.T) {
		e := newRealEnv(t)
		id := enrichedObject(t, e)
		e.useText(t, e.textLongWith(t, "sectioner", steps.NewSectioner()))
		before := e.get(t, id)
		_, _, err := e.svc.ReanalyzeObjectWith(ownerCtx(), id, ReanalyzeOptions{AllowNetwork: true})
		require.ErrorIs(t, err, upgrade.ErrCannotReanalyze)
		assert.Equal(t, before, e.get(t, id))
	})
}

// The Before hook (a caller's precheck of the stored object) runs after
// everything that refuses an object on its own terms, so an object those
// refuse is reported as refused, not as the precheck's error.
func TestReanalyzeCapability_BeforeRunsAfterTheRefusals(t *testing.T) {
	errPrecheck := errors.New("stored page too short")
	calls := 0
	opts := ReanalyzeOptions{Before: func(*storage.KnowledgeObject) error {
		calls++
		return errPrecheck
	}}
	e := newRealEnv(t)
	id := enrichedObject(t, e)
	e.useText(t, e.textLongWith(t, "sectioner", steps.NewSectioner())) // generative step without a provider
	_, _, err := e.svc.ReanalyzeObjectWith(ownerCtx(), id, opts)
	require.ErrorIs(t, err, upgrade.ErrCannotReanalyze)
	require.NotErrorIs(t, err, errPrecheck)
	require.Zero(t, calls, "the precheck ran before the refusal")

	// An object nothing refuses reaches the precheck, and its error stands.
	e = newRealEnv(t)
	id = enrichedObject(t, e)
	_, _, err = e.svc.ReanalyzeObjectWith(ownerCtx(), id, opts)
	require.ErrorIs(t, err, errPrecheck)
	require.Equal(t, 1, calls)
}

// With a working LLM the same object is processed.
func TestReanalyzeCapability_ProcessedWithLLM(t *testing.T) {
	e := newRealEnv(t)
	id := enrichedObject(t, e)
	calls := e.llmCalls()
	_, _, err := e.svc.ReanalyzeObject(ownerCtx(), id)
	require.NoError(t, err)
	assert.Greater(t, e.llmCalls(), calls, "the llm was not called")
	assert.Contains(t, tagSources(e.get(t, id).Tags), "llmtagone")
}

// A pipeline with no generative step is processed with no provider.
func TestReanalyzeCapability_NoGenerativeStepProcessed(t *testing.T) {
	e := newRealEnv(t)
	id := enrichedObject(t, e)
	orig, err := e.reg.Get(pipeline.TextLong)
	require.NoError(t, err)
	cp := *orig
	cp.Steps = orig.Steps[:1] // the type detector
	require.Empty(t, pipeline.GenerativeResourceOf(cp.Steps[0].Contract()))
	e.useText(t, &cp)
	calls := e.llmCalls()
	_, _, err = e.svc.ReanalyzeObject(ownerCtx(), id)
	require.NoError(t, err)
	assert.Equal(t, calls, e.llmCalls())
}

// A skip_steps override that removed the generative steps is the user's
// choice: processed, not refused, though the registry has no LLM.
func TestReanalyzeCapability_SkipStepsRemovingGenerativeStepsIsNotRefused(t *testing.T) {
	e := newRealEnv(t)
	id := enrichedObject(t, e)

	reg := noGenerativeRegistry(t, builtins.BuildOpts{})
	p, err := reg.Get(pipeline.TextLong)
	require.NoError(t, err)
	for _, st := range p.Steps {
		require.Empty(t, pipeline.GenerativeResourceOf(st.Contract()), "fixture: %s is generative", st.Name())
	}
	require.Empty(t, p.Pruned, "fixture: the skipped steps are not pruned")
	e.svc.Pipes = reg

	_, _, err = e.svc.ReanalyzeObject(ownerCtx(), id)
	assert.NotErrorIs(t, err, upgrade.ErrCannotReanalyze)
	require.NoError(t, err)
}

// stubLLMRegistry is the built-in registry over the stub LLM, with skip
// removed from text.long.
func stubLLMRegistry(t *testing.T, opts builtins.BuildOpts, skip []string) pipeline.Registry {
	t.Helper()
	stub := config.ProviderBackendConfig{Backend: config.BackendStub}
	cfg := config.ProvidersConfig{LLM: stub, Vision: stub}
	refuse := func(name string) (string, error) { return "", &providers.ToolNotInstalledError{Tool: name} }
	opts.Factory = providers.NewFactory(cfg, nil).WithContext(providers.WithToolLookup(context.Background(), refuse))
	reg, err := builtins.ConfiguredRegistryWithPipelineOverrides(opts, cfg,
		config.PipelinesConfig{Overrides: map[string]config.PipelineOverride{pipeline.TextLong: {SkipSteps: skip}}})
	require.NoError(t, err)
	return reg
}

// skip_steps removes the steps that hold the stub, but a generative step
// the definition still has was pruned for the missing capability: refused.
func TestReanalyzeCapability_PrunedStepAfterSkipStepsIsRefused(t *testing.T) {
	e := newRealEnv(t)
	id := enrichedObject(t, e)
	reg := stubLLMRegistry(t, builtins.BuildOpts{}, []string{"sectioner", "tagger", "entity_extractor", "entity_resolver"})
	p, err := reg.Get(pipeline.TextLong)
	require.NoError(t, err)
	require.NotEmpty(t, p.Pruned, "fixture: graph_extractor is pruned")
	e.svc.Pipes = reg
	e.refusedUnchanged(t, id, "llm provider", "step graph_extractor")
}

// noGenerativeRegistry is for tests of something else (vectors, metadata)
// that have no LLM: text.long without its generative steps, since a
// reanalysis of a pipeline that needs an LLM is refused.
func noGenerativeRegistry(t *testing.T, opts builtins.BuildOpts) pipeline.Registry {
	return stubLLMRegistry(t, opts, []string{"sectioner", "tagger", "entity_extractor", "entity_resolver", "graph_extractor", "structured_metadata", "c12n_classify"})
}

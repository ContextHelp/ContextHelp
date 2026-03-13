// Package autosuggest provides a pipeline plugin that uses an LLM to suggest
// tags and @mentions for knowledge objects post-enrichment.
package autosuggest

import (
	"context"
	"fmt"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/plugin"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Plugin is the top-level plugin.Plugin implementation for auto-suggest.
type Plugin struct {
	cfg  AutoSuggestConfig
	step *AutoSuggestStep
}

// New creates an uninitialized Plugin. Call via plugin.Registry.Register.
func New() *Plugin { return &Plugin{} }

func (p *Plugin) Name() string    { return "autosuggest" }
func (p *Plugin) Version() string { return "1.0.0" }

func (p *Plugin) Init(ctx context.Context, raw map[string]interface{}, deps plugin.Deps) error {
	cfg, err := ConfigFromMap(raw)
	if err != nil {
		return fmt.Errorf("autosuggest: config: %w", err)
	}
	p.cfg = cfg

	// deps.Store exposes the StorageDriver; deps.Bus is the event bus.
	// The LLM provider is not injected via Deps today — it requires *providers.Factory.
	// If Factory is not available here, the step is created without an LLM and falls back
	// to a noop (suggestions skipped). Callers should use InitWithLLM when possible.
	p.step = NewAutoSuggestStep(cfg, nil)
	return nil
}

// InitWithLLM creates the step with a real LLM provider.
func (p *Plugin) InitWithLLM(ctx context.Context, raw map[string]interface{}, deps plugin.Deps, llm LLMProvider) error {
	if err := p.Init(ctx, raw, deps); err != nil {
		return err
	}
	if llm != nil {
		p.step = NewAutoSuggestStep(p.cfg, llm)
	}
	return nil
}

func (p *Plugin) PipelineSteps() []pipeline.PipelineStep {
	if p.step == nil {
		return nil
	}
	return []pipeline.PipelineStep{p.step}
}

// PostIngest implements plugin.PostIngestHook.
// After the object is stored, the step is re-run if mode == "generate" and no tags were set.
// This is the event-driven path; the step also runs inline in the pipeline.
func (p *Plugin) PostIngest(ctx context.Context, obj *storage.KnowledgeObject) error {
	if p.step == nil || !p.cfg.Enabled {
		return nil
	}
	// Only run if we did not already enrich (e.g., autosuggest step was not in the pipeline).
	if p.cfg.Mode == "generate" && len(obj.Tags) == 0 {
		_, err := p.step.Run(ctx, obj)
		return err
	}
	return nil
}

func (p *Plugin) Close(_ context.Context) error { return nil }

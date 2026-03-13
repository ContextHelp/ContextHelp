package plugin

import (
	"context"
	"fmt"
	"log"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
)

// Registry holds registered plugins and coordinates their lifecycle.
type Registry struct {
	plugins []Plugin
}

// NewRegistry creates an empty Registry.
func NewRegistry() *Registry { return &Registry{} }

// Register adds a plugin. Panics on duplicate name.
func (r *Registry) Register(p Plugin) {
	for _, existing := range r.plugins {
		if existing.Name() == p.Name() {
			panic(fmt.Sprintf("plugin: duplicate name %q", p.Name()))
		}
	}
	r.plugins = append(r.plugins, p)
}

// InitAll initialises all registered plugins.
func (r *Registry) InitAll(ctx context.Context, cfgs map[string]map[string]interface{}, deps Deps) error {
	for _, p := range r.plugins {
		cfg := cfgs[p.Name()]
		if cfg == nil {
			cfg = map[string]interface{}{}
		}
		if err := p.Init(ctx, cfg, deps); err != nil {
			return fmt.Errorf("plugin %q init: %w", p.Name(), err)
		}
		log.Printf("plugin: %s %s initialised", p.Name(), p.Version())
	}
	return nil
}

// ExtraSteps collects all pipeline.PipelineStep contributions from every plugin.
func (r *Registry) ExtraSteps() []pipeline.PipelineStep {
	var out []pipeline.PipelineStep
	for _, p := range r.plugins {
		out = append(out, p.PipelineSteps()...)
	}
	return out
}

// PostIngestHooks returns all plugins that implement PostIngestHook.
func (r *Registry) PostIngestHooks() []PostIngestHook {
	var out []PostIngestHook
	for _, p := range r.plugins {
		if h, ok := p.(PostIngestHook); ok {
			out = append(out, h)
		}
	}
	return out
}

// AliasResolvers returns all plugins implementing AliasResolver.
func (r *Registry) AliasResolvers() []AliasResolver {
	var out []AliasResolver
	for _, p := range r.plugins {
		if ar, ok := p.(AliasResolver); ok {
			out = append(out, ar)
		}
	}
	return out
}

// ResolveID tries each registered AliasResolver in order, returning on first success.
// Falls back to returning the input unchanged.
func (r *Registry) ResolveID(ctx context.Context, idOrAlias, profile string) (string, error) {
	for _, ar := range r.AliasResolvers() {
		id, err := ar.ResolveID(ctx, idOrAlias, profile)
		if err == nil && id != "" && id != idOrAlias {
			return id, nil
		}
	}
	return idOrAlias, nil
}

// CloseAll shuts down all plugins gracefully.
func (r *Registry) CloseAll(ctx context.Context) {
	for _, p := range r.plugins {
		if err := p.Close(ctx); err != nil {
			log.Printf("plugin: %s close error: %v", p.Name(), err)
		}
	}
}

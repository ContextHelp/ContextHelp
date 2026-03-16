// Package aliasing provides a plugin for human-readable object aliases.
package aliasing

import (
	"context"
	"fmt"
	"time"

	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
)

// Plugin is the pluginapi.Plugin + pluginapi.AliasResolver implementation.
type Plugin struct {
	cfg   AliasConfig
	store pluginapi.AliasStore
}

// New returns an uninitialised Plugin for registration.
func New() *Plugin { return &Plugin{} }

func (p *Plugin) Name() string    { return "aliasing" }
func (p *Plugin) Version() string { return "1.0.0" }

func (p *Plugin) Init(_ context.Context, raw map[string]interface{}, deps pluginapi.Deps) error {
	cfg, err := ConfigFromMap(raw)
	if err != nil {
		return fmt.Errorf("aliasing: config: %w", err)
	}
	p.cfg = cfg
	if deps.Store != nil {
		p.store = deps.Store.Aliases()
	}
	return nil
}

// PipelineSteps returns nothing — aliasing is not a pipeline step.
func (p *Plugin) PipelineSteps() []pluginapi.PipelineStep { return nil }

func (p *Plugin) Close(_ context.Context) error { return nil }

// ResolveID implements pluginapi.AliasResolver.
func (p *Plugin) ResolveID(ctx context.Context, idOrAlias, profile string) (string, error) {
	if !p.cfg.Enabled || p.store == nil {
		return idOrAlias, nil
	}
	return ResolveAlias(ctx, p.store, idOrAlias, profile)
}

// SetAliasOp creates an alias via the plugin (called by REST/CLI handlers).
func (p *Plugin) SetAliasOp(ctx context.Context, alias, objectID, scope, profile string) error {
	if p.store == nil {
		return fmt.Errorf("aliasing: store not initialised")
	}
	if scope == "" {
		scope = p.cfg.DefaultScope
	}
	return SetAlias(ctx, p.store, alias, objectID, scope, profile, time.Now())
}

// ListAliases lists aliases matching the filter.
func (p *Plugin) ListAliases(ctx context.Context, objectID string) ([]*pluginapi.Alias, error) {
	if p.store == nil {
		return nil, nil
	}
	return p.store.List(ctx, pluginapi.AliasFilter{ObjectID: objectID})
}

// RemoveAlias deletes an alias.
func (p *Plugin) RemoveAlias(ctx context.Context, alias, scope, profile string) error {
	if p.store == nil {
		return fmt.Errorf("aliasing: store not initialised")
	}
	return p.store.Delete(ctx, alias, scope, profile)
}

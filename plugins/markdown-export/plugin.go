// Package markdownexport provides an Obsidian-compatible Markdown output-generator plugin.
package markdownexport

import (
	"context"
	"fmt"

	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
)

// Plugin implements pluginapi.Plugin + pluginapi.OutputGenerator.
type Plugin struct {
	cfg Config
}

// New returns an uninitialised Plugin ready for registration.
func New() *Plugin { return &Plugin{} }

// ── pluginapi.Plugin ──────────────────────────────────────────────────────────

func (p *Plugin) Name() string    { return "markdown-export" }
func (p *Plugin) Version() string { return "1.0.0" }

func (p *Plugin) Init(_ context.Context, raw map[string]interface{}, _ pluginapi.Deps) error {
	cfg, err := ConfigFromMap(raw)
	if err != nil {
		return fmt.Errorf("markdown-export: config: %w", err)
	}
	p.cfg = cfg
	return nil
}

// PipelineSteps returns nothing — this plugin is output-only, not a pipeline step.
func (p *Plugin) PipelineSteps() []pluginapi.PipelineStep { return nil }

func (p *Plugin) Close(_ context.Context) error { return nil }

// ── pluginapi.OutputGenerator ─────────────────────────────────────────────────

// GeneratorName returns the format identifier used with --format.
func (p *Plugin) GeneratorName() string { return "obsidian-md" }

// Accepts returns true for any KnowledgeObject when the plugin is enabled.
// A future version may restrict to text/url types only.
func (p *Plugin) Accepts(obj pluginapi.KnowledgeObject) bool {
	return p.cfg.Enabled && obj.ID != ""
}

// Generate renders obj as Obsidian-compatible Markdown.
// When opts.Destination is empty the plugin uses the configured VaultPath for
// informational purposes only; the rendered bytes are always returned to the
// caller which decides where to write them.
func (p *Plugin) Generate(_ context.Context, obj pluginapi.KnowledgeObject, _ pluginapi.OutputOptions) ([]byte, error) {
	if !p.cfg.Enabled {
		return nil, fmt.Errorf("markdown-export: plugin is disabled")
	}
	return Render(obj)
}

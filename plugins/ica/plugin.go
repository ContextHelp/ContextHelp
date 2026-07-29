// Package ica is the ICA integration plugin for ctxt.
//
// It connects ctxt to the ICA API and processor services, enabling
// content distribution and feed pipeline integration.
//
// Config (under plugins.ica):
//
//	api_url: https://api.example.com
//	processor_url: https://processor.example.com
//	default_feed_pipeline: false
//	dist_channels_as_mentions: false
package ica

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
)

const probeTimeout = 2 * time.Second

// StepFactory constructs pipeline steps for the plugin.
// Injected by the steps sub-package to break the import cycle.
type StepFactory struct {
	NewFeedManager func(
		apiURL string, client *http.Client,
	) pluginapi.PipelineStep
	NewFetcher func(
		apiURL string, client *http.Client,
	) pluginapi.PipelineStep
	NewNormalizer func(
		distChannelsAsMentions bool,
	) pluginapi.PipelineStep
	NewProcessor func(
		processorURL string, client *http.Client,
	) pluginapi.PipelineStep
}

// Plugin implements pluginapi.Plugin and pluginapi.PostIngestHook.
type Plugin struct {
	cfg  Config
	deps pluginapi.Deps

	client       *http.Client
	hasAPI       bool
	hasProcessor bool
	steps        StepFactory
}

// New returns an uninitialised Plugin for registration.
func New() *Plugin { return &Plugin{} }

// NewWithSteps returns a Plugin wired to the given step factory.
func NewWithSteps(sf StepFactory) *Plugin {
	return &Plugin{steps: sf}
}

// InitForTest sets config and client without probing health
// endpoints. Intended for unit tests only.
func (p *Plugin) InitForTest(cfg Config, client *http.Client) {
	p.cfg = cfg
	p.client = client
}

// ── pluginapi.Plugin ─────────────────────────────────────────────────────────

func (p *Plugin) Name() string    { return "ica" }
func (p *Plugin) Version() string { return "0.1.0" }

// Init reads config and probes upstream services.
func (p *Plugin) Init(ctx context.Context, raw map[string]interface{}, deps pluginapi.Deps) error {
	cfg, err := ConfigFromMap(raw)
	if err != nil {
		return fmt.Errorf("ica: config: %w", err)
	}
	p.cfg = cfg
	p.deps = deps
	p.client = &http.Client{Timeout: probeTimeout}

	if cfg.APIURL != "" {
		p.hasAPI = p.probe(ctx, cfg.APIURL+"/health")
	}
	if cfg.ProcessorURL != "" {
		p.hasProcessor = p.probe(ctx, cfg.ProcessorURL+"/health")
	}

	return nil
}

// PipelineSteps returns all ICA pipeline steps, gated by config.
func (p *Plugin) PipelineSteps() []pluginapi.PipelineStep {
	sf := p.steps
	if sf.NewNormalizer == nil {
		return nil // no step factory wired
	}

	var out []pluginapi.PipelineStep

	if p.cfg.APIURL != "" && sf.NewFeedManager != nil {
		out = append(out,
			sf.NewFeedManager(p.cfg.APIURL, p.client),
		)
	}
	if p.cfg.APIURL != "" && sf.NewFetcher != nil {
		out = append(out,
			sf.NewFetcher(p.cfg.APIURL, p.client),
		)
	}

	out = append(out,
		sf.NewNormalizer(p.cfg.DistChannelsAsMentions),
	)

	if p.cfg.ProcessorURL != "" && sf.NewProcessor != nil {
		out = append(out,
			sf.NewProcessor(p.cfg.ProcessorURL, p.client),
		)
	}

	return out
}

// Close is a no-op.
func (p *Plugin) Close(_ context.Context) error { return nil }

// ── pluginapi.PostIngestHook ─────────────────────────────────────────────────

// PostIngest is a no-op placeholder.
func (p *Plugin) PostIngest(_ context.Context, _ *pluginapi.KnowledgeObject) error {
	return nil
}

// ── capability gates ─────────────────────────────────────────────────────────

// HasAPI reports whether the ICA API is reachable.
func (p *Plugin) HasAPI() bool { return p.hasAPI }

// HasProcessor reports whether the ICA processor is reachable.
func (p *Plugin) HasProcessor() bool { return p.hasProcessor }

// ── internals ────────────────────────────────────────────────────────────────

// probe sends a GET to url and returns true on HTTP 200.
func (p *Plugin) probe(ctx context.Context, url string) bool {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		log.Printf("ica: probe %s: %v", url, err)
		return false
	}

	resp, err := p.client.Do(req)
	if err != nil {
		log.Printf("ica: probe %s: unreachable: %v", url, err)
		return false
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		log.Printf("ica: probe %s: HTTP %d", url, resp.StatusCode)
		return false
	}
	return true
}

// compile-time interface checks
var (
	_ pluginapi.Plugin         = (*Plugin)(nil)
	_ pluginapi.PostIngestHook = (*Plugin)(nil)
)

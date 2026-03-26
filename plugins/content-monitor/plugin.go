// Package contentmonitor is a reference plugin that fetches one or more URLs
// on a configurable schedule, detects content changes via SHA-256 hashing, and
// publishes a KnowledgeObject event whenever a change is detected.
//
// The first successful fetch per target is treated as baseline — no event is
// emitted on initial capture. Subsequent fetches that differ from the previous
// hash emit a "ctxt.plugin.content-monitor.change" event whose KnowledgeObject
// contains a line-level diff summary in TextContent.
//
// Config (under plugins.content-monitor):
//
//	urls:
//	  - https://example.com/pricing     # shorthand: label defaults to URL
//	targets:
//	  - url: https://example.com/docs
//	    label: Docs Page                # optional human label
//	interval: 30m
//	user_agent: "my-monitor/1"
//	timeout_seconds: 30
//	max_body_bytes: 524288
package contentmonitor

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
)

// targetState tracks last-seen content for a single URL.
type targetState struct {
	hash    string // hex SHA-256 of last body
	content string // last body (bounded by MaxBodyBytes)
}

// Plugin implements pluginapi.Plugin.
type Plugin struct {
	cfg  Config
	deps pluginapi.Deps

	mu     sync.Mutex
	states map[string]*targetState // keyed by URL

	cancel context.CancelFunc
	done   chan struct{}
}

// New returns an uninitialised Plugin for registration.
func New() *Plugin { return &Plugin{} }

// ── pluginapi.Plugin ──────────────────────────────────────────────────────────

func (p *Plugin) Name() string    { return "content-monitor" }
func (p *Plugin) Version() string { return "1.0.0" }

// Init reads config and starts the monitoring goroutine.
func (p *Plugin) Init(ctx context.Context, raw map[string]interface{}, deps pluginapi.Deps) error {
	cfg, err := ConfigFromMap(raw)
	if err != nil {
		return fmt.Errorf("content-monitor: config: %w", err)
	}
	p.cfg = cfg
	p.deps = deps
	p.states = make(map[string]*targetState)
	p.done = make(chan struct{})

	pctx, cancel := context.WithCancel(ctx)
	p.cancel = cancel
	go p.loop(pctx)
	return nil
}

// PipelineSteps returns nothing — this plugin is monitoring-only.
func (p *Plugin) PipelineSteps() []pluginapi.PipelineStep { return nil }

// Close stops the monitoring loop and waits for it to exit.
func (p *Plugin) Close(_ context.Context) error {
	if p.cancel != nil {
		p.cancel()
		<-p.done
	}
	return nil
}

// ── internals ─────────────────────────────────────────────────────────────────

func (p *Plugin) loop(ctx context.Context) {
	defer close(p.done)

	p.checkAll(ctx)

	ticker := time.NewTicker(p.cfg.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.checkAll(ctx)
		}
	}
}

func (p *Plugin) checkAll(ctx context.Context) {
	for _, t := range p.cfg.Targets {
		if err := p.checkTarget(ctx, t); err != nil {
			_ = p.publish(ctx, errEvent(t.URL, err))
		}
	}
}

func (p *Plugin) checkTarget(ctx context.Context, t URLTarget) error {
	body, err := p.fetch(ctx, t.URL)
	if err != nil {
		return err
	}

	sum := sha256.Sum256(body)
	newHash := fmt.Sprintf("%x", sum)
	newContent := string(body)

	p.mu.Lock()
	prev, exists := p.states[t.URL]
	if !exists {
		// First fetch — record baseline, emit a "seen" event but no diff.
		p.states[t.URL] = &targetState{hash: newHash, content: newContent}
		p.mu.Unlock()
		return p.publish(ctx, seenEvent(t, newHash, newContent))
	}

	if prev.hash == newHash {
		p.mu.Unlock()
		return nil // no change
	}

	oldContent := prev.content
	prev.hash = newHash
	prev.content = newContent
	p.mu.Unlock()

	summary, diff := diffSummary(oldContent, newContent)
	return p.publish(ctx, changeEvent(t, newHash, summary, diff, oldContent, newContent))
}

func (p *Plugin) fetch(ctx context.Context, url string) ([]byte, error) {
	client := &http.Client{
		Timeout: time.Duration(p.cfg.TimeoutSeconds) * time.Second,
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("content-monitor: build request for %s: %w", url, err)
	}
	req.Header.Set("User-Agent", p.cfg.UserAgent)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("content-monitor: fetch %s: %w", url, err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("content-monitor: fetch %s: HTTP %d", url, resp.StatusCode)
	}

	limited := io.LimitReader(resp.Body, p.cfg.MaxBodyBytes)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("content-monitor: read body for %s: %w", url, err)
	}
	return data, nil
}

func (p *Plugin) publish(ctx context.Context, e pluginapi.Event) error {
	if p.deps.Bus == nil {
		return nil
	}
	return p.deps.Bus.Publish(ctx, e)
}

// ── event constructors ────────────────────────────────────────────────────────

func seenEvent(t URLTarget, hash, content string) pluginapi.Event {
	obj := buildObject(t, hash, content, "baseline", "")
	return pluginapi.Event{
		ID:              hash,
		Source:          "plugin/content-monitor",
		SpecVersion:     "1.0",
		Type:            "ctxt.plugin.content-monitor.seen",
		DataContentType: "application/json",
		Time:            time.Now().UTC(),
		Data:            mustJSON(obj),
	}
}

func changeEvent(t URLTarget, newHash, summary, diff, _, newContent string) pluginapi.Event {
	obj := buildObject(t, newHash, newContent, "change", summary)
	if diff != "" {
		obj.Sections = append(obj.Sections, pluginapi.Section{
			Title:   "Diff",
			Content: diff,
			Order:   len(obj.Sections),
		})
	}
	return pluginapi.Event{
		ID:              newHash,
		Source:          "plugin/content-monitor",
		SpecVersion:     "1.0",
		Type:            "ctxt.plugin.content-monitor.change",
		DataContentType: "application/json",
		Time:            time.Now().UTC(),
		Data:            mustJSON(obj),
	}
}

func errEvent(url string, err error) pluginapi.Event {
	sum := sha256.Sum256([]byte(url + err.Error()))
	return pluginapi.Event{
		ID:          fmt.Sprintf("%x", sum),
		Source:      "plugin/content-monitor",
		SpecVersion: "1.0",
		Type:        "ctxt.plugin.content-monitor.error",
		Time:        time.Now().UTC(),
		Data:        mustJSON(map[string]string{"url": url, "error": err.Error()}),
	}
}

func buildObject(t URLTarget, hash, content, subtype, summary string) pluginapi.KnowledgeObject {
	now := time.Now().UTC()
	obj := pluginapi.KnowledgeObject{
		ID:          hash,
		Type:        "url",
		Subtype:     "content-monitor-" + subtype,
		RawContent:  t.URL,
		TextContent: content,
		ContentType: "text/html",
		Source:      "content-monitor",
		ContentHash: hash,
		Metadata: map[string]any{
			"url":   t.URL,
			"label": t.Label,
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if summary != "" {
		obj.Sections = []pluginapi.Section{
			{Title: "Change Summary", Content: summary, Order: 0},
		}
		obj.Metadata["change_summary"] = summary
	}
	return obj
}

func mustJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v) //nolint:errcheck
	return b
}

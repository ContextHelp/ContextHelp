// Package dirwatcher is a reference ingestion plugin that polls a directory for
// new files and publishes each as a KnowledgeObject via the pluginapi event bus.
//
// The watcher uses a pure-Go polling approach (no fsnotify dependency) so it
// works without CGO and on any OS. Files are identified by path; once ingested
// the path is remembered for the session to avoid re-ingestion.
//
// Config (under plugins.dir-watcher):
//
//	dir: /tmp/drop             # required — directory to watch
//	interval: 10s              # poll interval (default 10s)
//	extensions: [".txt",".md"] # allowlist; empty = all files
//	max_file_size_bytes: 1048576  # cap per file (default 1 MiB)
//	delete_after_ingest: false  # move ingested files to .ctxt-ingested/
package dirwatcher

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
)

// Plugin implements pluginapi.Plugin.
type Plugin struct {
	cfg  Config
	deps pluginapi.Deps

	mu       sync.Mutex
	ingested map[string]struct{} // relative paths ingested this session

	cancel context.CancelFunc
	done   chan struct{}
}

// New returns an uninitialised Plugin for registration.
func New() *Plugin { return &Plugin{} }

// ── pluginapi.Plugin ──────────────────────────────────────────────────────────

func (p *Plugin) Name() string    { return "dir-watcher" }
func (p *Plugin) Version() string { return "1.0.0" }

// Init reads config and starts the watcher goroutine.
func (p *Plugin) Init(ctx context.Context, raw map[string]interface{}, deps pluginapi.Deps) error {
	cfg, err := ConfigFromMap(raw)
	if err != nil {
		return fmt.Errorf("dir-watcher: config: %w", err)
	}
	p.cfg = cfg
	p.deps = deps
	p.ingested = make(map[string]struct{})
	p.done = make(chan struct{})

	pctx, cancel := context.WithCancel(ctx)
	p.cancel = cancel
	go p.loop(pctx)
	return nil
}

// PipelineSteps returns nothing — this plugin is ingestion-only.
func (p *Plugin) PipelineSteps() []pluginapi.PipelineStep { return nil }

// Close stops the watcher and waits for it to exit.
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

	p.scan(ctx)

	ticker := time.NewTicker(p.cfg.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.scan(ctx)
		}
	}
}

func (p *Plugin) scan(ctx context.Context) {
	entries, err := os.ReadDir(p.cfg.Dir)
	if err != nil {
		_ = p.publish(ctx, errEvent(p.cfg.Dir, err))
		return
	}

	for _, de := range entries {
		if de.IsDir() {
			continue
		}
		name := de.Name()
		if !p.shouldIngest(name) {
			continue
		}
		relPath := filepath.Join(p.cfg.Dir, name)
		if p.alreadyIngested(relPath) {
			continue
		}

		obj, err := p.fileToObject(relPath, de)
		if err != nil {
			_ = p.publish(ctx, errEvent(relPath, err))
			continue
		}

		if err := p.publish(ctx, objectEvent(obj)); err != nil {
			continue
		}
		p.markIngested(relPath)

		if p.cfg.DeleteAfterIngest {
			p.moveToIngested(relPath)
		}
	}
}

func (p *Plugin) shouldIngest(name string) bool {
	if len(p.cfg.Extensions) == 0 {
		return true
	}
	ext := strings.ToLower(filepath.Ext(name))
	for _, allowed := range p.cfg.Extensions {
		if strings.ToLower(allowed) == ext {
			return true
		}
	}
	return false
}

func (p *Plugin) alreadyIngested(path string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	_, ok := p.ingested[path]
	return ok
}

func (p *Plugin) markIngested(path string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.ingested[path] = struct{}{}
}

func (p *Plugin) fileToObject(path string, de os.DirEntry) (pluginapi.KnowledgeObject, error) {
	info, err := de.Info()
	if err != nil {
		return pluginapi.KnowledgeObject{}, fmt.Errorf("dir-watcher: stat %s: %w", path, err)
	}

	var content []byte
	if p.cfg.MaxFileSizeBytes > 0 && info.Size() > p.cfg.MaxFileSizeBytes {
		// Partial read up to cap.
		f, err := os.Open(path) //nolint:gosec // path from trusted config dir
		if err != nil {
			return pluginapi.KnowledgeObject{}, fmt.Errorf("dir-watcher: open %s: %w", path, err)
		}
		defer f.Close() //nolint:errcheck
		content, err = io.ReadAll(io.LimitReader(f, p.cfg.MaxFileSizeBytes))
		if err != nil {
			return pluginapi.KnowledgeObject{}, fmt.Errorf("dir-watcher: read %s: %w", path, err)
		}
	} else {
		content, err = os.ReadFile(path) //nolint:gosec // path from trusted config dir
		if err != nil {
			return pluginapi.KnowledgeObject{}, fmt.Errorf("dir-watcher: read %s: %w", path, err)
		}
	}

	sum := sha256.Sum256(content)
	id := fmt.Sprintf("%x", sum)
	ct := detectContentType(de.Name(), content)
	now := time.Now().UTC()

	obj := pluginapi.KnowledgeObject{
		ID:          id,
		Type:        "file",
		Subtype:     strings.TrimPrefix(filepath.Ext(de.Name()), "."),
		RawContent:  path,
		TextContent: string(content),
		ContentType: ct,
		Source:      "dir-watcher:" + p.cfg.Dir,
		ContentHash: id,
		Metadata: map[string]any{
			"filename":   de.Name(),
			"path":       path,
			"size_bytes": info.Size(),
			"modified":   info.ModTime().Format(time.RFC3339),
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	// Populate graph-canonical nodes so downstream projection works correctly.
	if string(content) != "" {
		obj.Graph = &pluginapi.ObjectGraph{
			Nodes: []pluginapi.GraphNode{
				{
					ID:       pluginapi.NewNodeID(id, pluginapi.NodeTypeSummary, 0),
					NodeType: pluginapi.NodeTypeSummary,
					Content:  string(content),
					Order:    0,
				},
			},
		}
	}
	return obj, nil
}

func (p *Plugin) moveToIngested(path string) {
	dir := filepath.Dir(path)
	destDir := filepath.Join(dir, ".ctxt-ingested")
	_ = os.MkdirAll(destDir, 0o750)
	dest := filepath.Join(destDir, filepath.Base(path))
	_ = os.Rename(path, dest) //nolint:errcheck
}

func (p *Plugin) publish(ctx context.Context, e pluginapi.Event) error {
	if p.deps.Bus == nil {
		return nil
	}
	return p.deps.Bus.Publish(ctx, e)
}

// ── helpers ───────────────────────────────────────────────────────────────────

func objectEvent(obj pluginapi.KnowledgeObject) pluginapi.Event {
	return pluginapi.Event{
		ID:              obj.ID,
		Source:          "plugin/dir-watcher",
		SpecVersion:     "1.0",
		Type:            "ctxt.plugin.dir-watcher.file",
		DataContentType: "application/json",
		Time:            time.Now().UTC(),
		Data:            mustJSON(obj),
	}
}

func errEvent(path string, err error) pluginapi.Event {
	id := fmt.Sprintf("%x", sha256.Sum256([]byte(path+err.Error())))
	return pluginapi.Event{
		ID:          id,
		Source:      "plugin/dir-watcher",
		SpecVersion: "1.0",
		Type:        "ctxt.plugin.dir-watcher.error",
		Time:        time.Now().UTC(),
		Data:        mustJSON(map[string]string{"path": path, "error": err.Error()}),
	}
}

func detectContentType(name string, content []byte) string {
	ext := filepath.Ext(name)
	if ct := mime.TypeByExtension(ext); ct != "" {
		return ct
	}
	// Fallback: sniff first 512 bytes.
	if len(content) > 512 {
		return http.DetectContentType(content[:512])
	}
	return http.DetectContentType(content)
}

func mustJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v) //nolint:errcheck
	return b
}

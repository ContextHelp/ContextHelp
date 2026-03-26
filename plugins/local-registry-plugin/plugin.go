// Package localregistry is a reference PluginRegistryProvider that reads
// entity and taxonomy definitions from a directory of YAML files.
//
// Directory layout:
//
//	<dir>/
//	  taxonomy.yaml         — list of TaxonomyEntry
//	  entities/
//	    <any-name>.yaml     — list of Entity
//
// Config keys:
//
//	dir       string  — path to the registry directory (required)
//	name      string  — registry name (default: "local")
//	version   string  — registry version (default: "1.0.0")
//	namespace string  — default namespace applied when entity.Namespace is empty
package localregistry

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
	"gopkg.in/yaml.v3"
)

// Plugin implements pluginapi.Plugin + pluginapi.PluginRegistryProvider.
type Plugin struct {
	dir       string
	name      string
	version   string
	namespace string
}

// New returns an uninitialised Plugin for registration.
func New() *Plugin { return &Plugin{} }

func (p *Plugin) Name() string    { return "local-registry" }
func (p *Plugin) Version() string { return "1.0.0" }

// Init reads the plugin config block.
func (p *Plugin) Init(_ context.Context, cfg map[string]interface{}, _ pluginapi.Deps) error {
	p.dir = stringKey(cfg, "dir", "")
	if p.dir == "" {
		return fmt.Errorf("local-registry: config key 'dir' is required")
	}
	p.name = stringKey(cfg, "name", "local")
	p.version = stringKey(cfg, "version", "1.0.0")
	p.namespace = stringKey(cfg, "namespace", "")
	return nil
}

// PipelineSteps returns nothing — registry providers are not pipeline steps.
func (p *Plugin) PipelineSteps() []pluginapi.PipelineStep { return nil }

// Close is a no-op; no resources held.
func (p *Plugin) Close(_ context.Context) error { return nil }

// ─── PluginRegistryProvider ──────────────────────────────────────────────────

// Metadata returns the registry descriptor.
func (p *Plugin) Metadata(_ context.Context) (pluginapi.RegistryDescriptor, error) {
	tax, err := p.loadTaxonomy()
	if err != nil {
		return pluginapi.RegistryDescriptor{}, err
	}
	ns := make([]string, 0, len(tax))
	for _, t := range tax {
		ns = append(ns, t.Namespace)
	}
	return pluginapi.RegistryDescriptor{
		Name:       p.name,
		Namespaces: ns,
		Version:    p.version,
	}, nil
}

// FetchTaxonomy loads taxonomy.yaml from the registry directory.
func (p *Plugin) FetchTaxonomy(_ context.Context) (pluginapi.Taxonomy, error) {
	return p.loadTaxonomy()
}

// FetchEntities returns all entities from the entities/ sub-directory.
// Pagination is not required for a local provider; cursor is ignored and
// nextCursor is always empty (single page).
func (p *Plugin) FetchEntities(_ context.Context, _ string) ([]pluginapi.Entity, string, error) {
	entDir := filepath.Join(p.dir, "entities")
	entries, err := os.ReadDir(entDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, "", nil
		}
		return nil, "", fmt.Errorf("local-registry: read entities dir: %w", err)
	}

	var all []pluginapi.Entity
	for _, de := range entries {
		if de.IsDir() || filepath.Ext(de.Name()) != ".yaml" {
			continue
		}
		batch, readErr := loadEntitiesFile(filepath.Join(entDir, de.Name()))
		if readErr != nil {
			return nil, "", readErr
		}
		for i := range batch {
			if batch[i].Namespace == "" {
				batch[i].Namespace = p.namespace
			}
		}
		all = append(all, batch...)
	}
	return all, "", nil
}

// ResolveEntity looks up a single entity by namespace + slug across all YAML files.
func (p *Plugin) ResolveEntity(ctx context.Context, namespace, slug string) (*pluginapi.Entity, error) {
	entities, _, err := p.FetchEntities(ctx, "")
	if err != nil {
		return nil, err
	}
	for i := range entities {
		e := &entities[i]
		if e.Slug == slug && e.Namespace == namespace {
			return e, nil
		}
	}
	return nil, nil
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func (p *Plugin) loadTaxonomy() (pluginapi.Taxonomy, error) {
	path := filepath.Join(p.dir, "taxonomy.yaml")
	data, err := os.ReadFile(path) //nolint:gosec // path derived from trusted config
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("local-registry: read taxonomy.yaml: %w", err)
	}
	var tax pluginapi.Taxonomy
	if err := yaml.Unmarshal(data, &tax); err != nil {
		return nil, fmt.Errorf("local-registry: parse taxonomy.yaml: %w", err)
	}
	return tax, nil
}

func loadEntitiesFile(path string) ([]pluginapi.Entity, error) {
	data, err := os.ReadFile(path) //nolint:gosec // path derived from trusted config
	if err != nil {
		return nil, fmt.Errorf("local-registry: read %s: %w", path, err)
	}
	var entities []pluginapi.Entity
	if err := yaml.Unmarshal(data, &entities); err != nil {
		return nil, fmt.Errorf("local-registry: parse %s: %w", path, err)
	}
	return entities, nil
}

func stringKey(m map[string]interface{}, key, def string) string {
	v, ok := m[key]
	if !ok {
		return def
	}
	s, _ := v.(string)
	if s == "" {
		return def
	}
	return s
}

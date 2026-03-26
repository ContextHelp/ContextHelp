// Package pluginapi — RegistryProvider interface for registry-provider plugins.
package pluginapi

import "context"

// RegistryDescriptor describes a plugin-backed registry (name, namespaces, version).
type RegistryDescriptor struct {
	// Name is the human-readable registry name.
	Name string
	// Namespaces lists the entity namespaces this registry owns.
	Namespaces []string
	// Version is the registry's semver string.
	Version string
}

// TaxonomyEntry is a single namespace node in a registry's taxonomy.
type TaxonomyEntry struct {
	Namespace   string `yaml:"namespace" json:"namespace"`
	Title       string `yaml:"title"     json:"title"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
}

// Taxonomy is the full set of taxonomy entries for a registry.
type Taxonomy []TaxonomyEntry

// Entity is the pluginapi view of a named entity — a subset of storage.Entity
// with only the fields plugin-backed registries need to supply.
// The sync layer maps these to storage.Entity before writing.
type Entity struct {
	Slug        string   `yaml:"slug"         json:"slug"`
	Title       string   `yaml:"title"        json:"title"`
	Description string   `yaml:"description,omitempty" json:"description,omitempty"`
	Namespace   string   `yaml:"namespace"    json:"namespace"`
	Aliases     []string `yaml:"aliases,omitempty" json:"aliases,omitempty"`
	VersionHash string   `yaml:"version_hash,omitempty" json:"version_hash,omitempty"`
}

// PluginRegistryProvider is the optional interface a plugin implements to act
// as a registry source alongside HTTP registries.
//
// Plugins that expose this interface must declare type: registry_provider in
// their manifest.yaml. The sync layer discovers them via
// plugin.Registry.RegistryProviders() and iterates all providers uniformly.
type PluginRegistryProvider interface {
	Plugin
	// Metadata returns the registry descriptor (name, namespaces, version).
	Metadata(ctx context.Context) (RegistryDescriptor, error)
	// FetchTaxonomy returns the full taxonomy for this registry.
	FetchTaxonomy(ctx context.Context) (Taxonomy, error)
	// FetchEntities returns all entity definitions (paginated).
	// cursor is empty on first call; pass the returned nextCursor on subsequent
	// calls until nextCursor is empty (signals end of results).
	FetchEntities(ctx context.Context, cursor string) ([]Entity, string, error)
	// ResolveEntity looks up a single entity by namespace + slug.
	// Returns nil, nil when not found.
	ResolveEntity(ctx context.Context, namespace, slug string) (*Entity, error)
}

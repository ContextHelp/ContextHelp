package plugin

import (
	"context"

	"github.com/ideacrafterslabs/ctxt/internal/events"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Plugin is the interface every plugin must satisfy.
type Plugin interface {
	// Name returns the unique plugin identifier (matches config key).
	Name() string
	// Version returns the plugin's semver string.
	Version() string
	// Init is called once at startup with the plugin's config block and shared deps.
	Init(ctx context.Context, cfg map[string]interface{}, deps Deps) error
	// PipelineSteps returns zero or more pipeline steps to register.
	PipelineSteps() []pipeline.PipelineStep
	// Close is called on graceful shutdown.
	Close(ctx context.Context) error
}

// PostIngestHook is implemented by plugins that want to run after an object is created.
type PostIngestHook interface {
	Plugin
	PostIngest(ctx context.Context, obj *storage.KnowledgeObject) error
}

// AliasResolver is implemented by plugins that can resolve aliases to object IDs.
type AliasResolver interface {
	Plugin
	// ResolveID resolves an alias (or passes through an ID unchanged).
	// Returns the canonical object ID or the input unchanged if not an alias.
	ResolveID(ctx context.Context, idOrAlias, profile string) (string, error)
}

// Deps carries shared dependencies injected at plugin init.
type Deps struct {
	Bus   events.Bus
	Store storage.StorageDriver
}

package plugin

import (
	"context"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
)

// Plugin is the interface every plugin must satisfy.
// This is a type alias for pluginapi.Plugin so external plugins and internal
// code use the same type.
type Plugin = pluginapi.Plugin

// PostIngestHook is implemented by plugins that want to run after an object is created.
type PostIngestHook interface {
	Plugin
	PostIngest(ctx context.Context, obj *storage.KnowledgeObject) error
}

// AliasResolver is implemented by plugins that can resolve aliases to object IDs.
// This is a type alias for pluginapi.AliasResolver.
type AliasResolver = pluginapi.AliasResolver

// OutputGenerator is implemented by plugins that render a KnowledgeObject to
// a specific format. This is a type alias for pluginapi.OutputGenerator.
type OutputGenerator = pluginapi.OutputGenerator

// OutputOptions carries rendering preferences. Type alias for pluginapi.OutputOptions.
type OutputOptions = pluginapi.OutputOptions

// Deps carries shared dependencies injected at plugin init.
// This is a type alias for pluginapi.Deps.
type Deps = pluginapi.Deps

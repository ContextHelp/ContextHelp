# pkg/pluginapi

Public API surface for ctxt plugins. Third-party plugin modules import only this package.

## Import path

```go
import "github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
```

## Implement a plugin

Every plugin must satisfy `pluginapi.Plugin`:

```go
type MyPlugin struct{}

func (p *MyPlugin) Name() string    { return "myplugin" }
func (p *MyPlugin) Version() string { return "1.0.0" }

func (p *MyPlugin) Init(_ context.Context, cfg map[string]interface{}, deps pluginapi.Deps) error {
    // deps.Store.Aliases() — access the alias store
    // deps.Bus             — publish / subscribe to events
    return nil
}

func (p *MyPlugin) PipelineSteps() []pluginapi.PipelineStep { return nil }
func (p *MyPlugin) Close(_ context.Context) error           { return nil }
```

## Optional interfaces

| Interface | When to implement |
|---|---|
| `pluginapi.AliasResolver` | Plugin resolves human-readable aliases to object IDs |
| `pluginapi.PostIngestHook` | Plugin runs logic after every ingested object is created |
| `pluginapi.PipelineStep` | Plugin contributes a transformation step to a pipeline |

## Deps

`pluginapi.Deps` is passed to `Init`. Fields:

- `Store pluginapi.StorageDriver` — narrow storage view; use `Store.Aliases()` to access alias persistence.
- `Bus pluginapi.Bus` — publish and subscribe to CloudEvents.

## Module setup (local development)

```
require github.com/ideacrafterslabs/ctxt v0.0.0

replace github.com/ideacrafterslabs/ctxt => ../..
```

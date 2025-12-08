# Plugin API Specification

This document defines the **official, stable Plugin API** for ContextHelp.
It describes all extension points, hooks, storage mechanisms, configuration rules, and interaction patterns available to plugins.
The goal is to guarantee that **plugins never require changes to core** and can safely provide powerful functionality—pipelines, job generation, notifications, semantic extensions, refresh rules, etc.—in a controlled, sandboxed manner.

This API is versioned, documented, and treated as a compatibility contract.

---

# 1. Overview

Plugins extend ContextHelp without modifying core functionality.
Plugins may:

- register custom pipelines
- attach pre/post ingestion hooks
- enqueue jobs
- define custom bookmark types
- store plugin-specific metadata
- manage their own state (JSON files)
- inject behavior into CLI/REST/gRPC output
- communicate with other plugins via local APIs
- participate in refresh scheduling
- generate alerts or notifications

Plugins **cannot**:

- modify core database schema
- override core security rules
- alter core bookmark schema fields
- modify or intercept internal system files
- access registers or storage without permission

---

# 2. Plugin Directory & Layout

Each plugin is stored under:

```
~/.contexthelp/plugins/<plugin-name>/
```

Recommended structure:

```
<plugin-name>/
  plugin.json          # manifest / metadata
  state.json           # plugin-owned storage
  cache/               # optional plugin caches
  logs/                # plugin logs
  code.so / code.js    # compiled or script extension
```

Plugins must not read or write outside this sandbox unless explicitly configured by user.

---

# 3. Plugin Manifest (plugin.json)

Plugins declare:

```
{
  "name": "rss_feed",
  "version": "1.0.0",
  "description": "Core plugin for RSS/Atom/JSONFeed ingestion",
  "entrypoint": "code.so",
  "hooks": ["post_ingest", "post_refresh", "on_cli_start"],
  "pipelines": ["feed.fetch", "feed.parse", "feed.ingest_items"],
  "bookmark_types": ["feed", "feed_item"],
  "permissions": {
    "network": true,
    "filesystem": true
  }
}
```

Manifest governs:

- load behavior
- security permissions
- allowed hooks
- declared pipelines
- declared bookmark types

---

# 4. Plugin Storage API

Plugins must store state inside their sandbox under `state.json`.

## Read State

```
plugin.State() -> map[string]interface{}
```

## Write State

```
plugin.WriteState(data interface{}) -> error
```

## Append to Log

```
plugin.Log(level, message)
```

## Recommended Pattern

- All plugin-maintained structured data stored as JSON
- Use versioning (`state_version`) for migrations
- All sensitive states encrypted or hashed if appropriate

---

# 5. Plugin Configuration API

Plugins may define configuration using:

```
configuration:
  plugins:
    rss_feed:
      default_interval_seconds: 3600
      ...
```

API:

```
plugin.Config() -> map[string]interface{}
plugin.ConfigTyped(&struct) -> error
```

Bookmark-level plugin config:

```
bookmark.plugins.<pluginName>
```

Access:

```
plugin.BookmarkConfig(bookmark, "rss_feed") -> interface{}
```

---

# 6. Plugin Hook System

This is the core extension mechanism.

## 6.1. pre_ingest

Run **before** pipelines start.

Use cases:

- infer custom types
- attach plugin metadata
- override pipeline selection

Signature:

```
func PreIngest(input IngestInput) IngestInput
```

---

## 6.2. post_ingest

Run after ingestion and bookmark creation.

Use cases:

- RSS plugin: detect feed entries and enqueue jobs
- Price monitor: extract price and track changes
- Notification plugin: emit notifications
- Enrich bookmark metadata

Signature:

```
func PostIngest(b Bookmark) error
```

---

## 6.3. post_pipeline_step

Runs after each pipeline step.

Use cases:

- monitoring
- semantic enrichment
- plugin-specific metrics

Signature:

```
func PostPipelineStep(ctx PipelineContext, step PipelineStep) error
```

---

## 6.4. post_refresh

Triggered after refresh job completes.

Use cases:

- price monitoring
- feed update detection
- resetting refresh intervals

Signature:

```
func PostRefresh(original Bookmark, refreshed Bookmark) error
```

---

## 6.5. on_cli_start

Allows plugins to inject content on each CLI command invocation.

Use cases:

- Notification plugin showing unread messages
- Status messages for long-running processes

Signature:

```
func OnCLIStart(context CLIContext) ([]CLIInjection, error)
```

---

## 6.6. on_rest_response

Modify or annotate REST responses.

Use cases:

- inject pending notifications
- plugin response enrichment

Signature:

```
func OnRESTResponse(resp *RESTResponse) error
```

---

## 6.7. on_grpc_response

Same as above, but for gRPC.

```
func OnGRPCResponse(resp *GRPCResponse) error
```

---

## 6.8. scheduled hooks (cron-style)

Plugins may request scheduled execution:

```
plugin.Schedule("price-monitor", intervalSeconds)
```

Triggers:

```
func ScheduledTask(name string) error
```

Enables:

- periodic price checks
- feed scanning
- analytics
- cleanup tasks

---

# 7. Pipeline API

Plugins may define new pipelines or hook into pipeline inference.

### Register Pipeline

```
plugin.RegisterPipeline(name string, steps []PipelineStepDefinition)
```

### Add inference rule

```
plugin.RegisterInferenceRule(func(IngestInput) (string, bool))
```

Allows:

- auto-detect feeds
- auto-detect podcast items
- custom domain logic

---

# 8. Job API

### Enqueue Analysis Job

```
plugin.EnqueueAnalyze(url string, pipeline string, metadata map[string]interface{})
```

### Enqueue Refresh Job

```
plugin.EnqueueRefresh(bookmarkID string)
```

### Enqueue Plugin Job

```
plugin.EnqueueJob("rss_fetch", payload)
```

Plugins NEVER modify the job schema.

---

# 9. Bookmark API

Plugins may read and write plugin-specific metadata in bookmarks.

### Read

```
plugin.BookmarkGet(id string) -> Bookmark
```

### Write plugin-specific field

```
plugin.BookmarkSetPluginField(id, pluginName string, data interface{})
```

Plugins must not write into core bookmark fields except:

- hints
- tags
- mentions

when explicitly intended.

---

# 10. Refresh Integration API

Plugins may request refresh scheduling for bookmarks.

### Enable Refresh

```
refresh.Enable(bookmarkID)
```

### Set Interval

```
refresh.SetInterval(bookmarkID, seconds)
```

### Force Minimum Interval
Used by price monitoring / feeds:

```
refresh.RequireMinimum(bookmarkID, seconds)
```

### Enable Auto-Fetch New Items

```
refresh.SetFetchNew(bookmarkID, true)
```

Plugins do not modify refresh internals—only call this API.

---

# 11. Notification API (Provided by Notification Plugin)

### Create Notification

```
notifications.Create(type, level, payload)
```

### Retrieve Pending Notifications

```
notifications.Pending() -> []Notification
```

### Mark Notifications As Read

```
notifications.MarkRead(id)
```

Inter-plugin API—requires plugin to opt into Notification Plugin.

---

# 12. Inter-Plugin Communication

Plugins may expose APIs to other plugins via runtime registry:

### Register API

```
plugin.ExportAPI("notifications", NotificationAPI)
```

### Import API

```
notif := plugin.ImportAPI("notifications")
```

Only explicit, opt-in cross-plugin calls allowed.

---

# 13. Security Model

Plugins must declare permissions:

```
"permissions": {
  "network": true,
  "filesystem": false
}
```

Core enforces:

- no network if disabled
- no filesystem access outside plugin sandbox
- no direct DB access
- no mutation of registry data

Plugins operate under least-privilege.

---

# 14. CLI / REST / gRPC Injection API

### CLI

Plugins may inject blocks of text:

```
return []CLIInjection{
  {Position: "before_output", Text: "..."},
}
```

### REST

```
resp.AddPluginData("notifications", pendingList)
```

### gRPC

```
resp.Attach("plugin", data)
```

---

# 15. Logging & Telemetry API

```
plugin.LogInfo("...")
plugin.LogWarn("...")
plugin.LogError("...")
```

Telemetry is sandboxed and plugin-specific.

---

# 16. Recommended Best Practices

- Keep state small and scoped
- Never block hooks (asynchronous recommended)
- Provide schema versioning for plugin data
- Avoid tight coupling between plugins
- Expose explicit APIs if needed

---

# 17. Versioning

Plugin API version:

```
plugin_api_version = "1.0"
```

Plugins must declare compatibility:

```
"requires_plugin_api": ">=1.0,<2.0"
```

---

# Summary

The Plugin API enables plugins to:

- define new pipelines
- define custom bookmark types
- store data independently
- enqueue jobs
- modify refresh rules
- inject notifications
- augment CLI, REST, and gRPC
- operate fully independently of core
- collaborate with other plugins safely

This ensures the entire ContextHelp ecosystem remains **modular**, **extensible**, and **stable**, without requiring core changes for new capabilities.
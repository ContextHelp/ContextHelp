# Price Monitor Plugin (design example, not buildable)

This page described a plugin that extracts prices from matching URLs during ingestion or refresh and alerts on drops, relying on the Refresh and Notification plugins. Those plugins are design only ([../plugins-refresh.md](../plugins-refresh.md), [../plugins-notifications.md](../plugins-notifications.md)), and no plugin is loaded at runtime ([../plugins.md](../plugins.md#current-state)).

## Closest real code

- `plugins/content-monitor`: fetches URLs on an interval, hashes the body, and emits a change object with a line diff. It is a reference plugin exercised by its own tests; nothing loads it, so its `plugins.content-monitor` config has no effect.
- A post-processing `PipelineStep` that reads a price from captured HTML can be written against `pkg/pluginapi`; see [../patterns-url-adapter-post-processor.md](../patterns-url-adapter-post-processor.md) and the [plugin authoring guide](../../plugin-authoring-guide.md).

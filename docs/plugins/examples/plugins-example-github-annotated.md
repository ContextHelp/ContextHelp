# GitHub Plugin Walkthrough (design example, not buildable)

This page walked through a GitHub PR-review watcher plugin: a `~/.contexthelp/plugins/<name>/` directory, a `plugin.json` manifest with hooks, entrypoint and syscall-blocking permissions, config under `plugins.<name>`, scheduled polling and a webhook server. That runtime does not exist:

- No plugin is loaded at runtime; there is no plugin directory, `.so` loading, scheduler hook or sandbox ([../plugins.md](../plugins.md#current-state)).
- The manifest is `manifest.json` / `manifest.yaml` with `apiVersion: v1`, `name`, `version`, optional `slug`, `description`, `type`, `permissions` ([authoring guide §2](../../plugin-authoring-guide.md#2-plugin-manifest)).
- `plugins.<name>` config is read by no code.

## GitHub today

- `ctxt import github --username <user> [--lists starred,watched,contributed]` imports repositories.
- Captured GitHub URLs route to built-in pipelines: `url.github.repo`, `url.github.pr`, `url.github.issue`, `url.github.release`, `url.github.profile`, `url.github.starred`.

## Building a plugin

Follow the [plugin authoring guide](../../plugin-authoring-guide.md): implement `pluginapi.Plugin` (and optionally `PipelineStep`, `PostIngestHook`, ...), test it in its own package, and register it by hand in an integration test, as the bundled plugins under `plugins/` do.

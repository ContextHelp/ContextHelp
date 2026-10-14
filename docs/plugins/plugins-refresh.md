# Refresh Plugin (design example, not buildable)

This page described a plugin that re-fetches stored objects and polls podcasts, channels, feeds and GitHub on regex-matched schedules, with its own CLI, REST and gRPC surface. None of it can run today:

- No plugin is loaded at runtime, and the `plugins:` config key is read by no code ([plugins.md](plugins.md#current-state)).
- Plugins cannot add CLI commands, REST routes or gRPC methods.

## What exists instead

| Need | Today |
|------|-------|
| Follow an RSS/Atom feed | `ctxt feed add <url>`, then `ctxt feed sync --id <id>` or `--url <url>` (or `POST /api/v1/feeds/sync` for every active feed). Nothing syncs feeds on a timer; schedule the command yourself |
| Re-run enrichment on a stored object | `ctxt reprocess <id> --step structured_metadata|entity_extractor|tagger` |
| Regenerate an entity page | `ctxt page refresh <slug>` |
| Watch local directories | `ctxt watch` ([ambient.md](../ambient.md#what-runs-today)) |
| Browser tabs and history on a timer (macOS) | `ctxt capture schedule install` |

The bundled `plugins/rss-feed` and `plugins/content-monitor` packages implement polling as plugins, but nothing loads them.

## Building a plugin

Start with [plugins.md](plugins.md) and the [plugin authoring guide](../plugin-authoring-guide.md).

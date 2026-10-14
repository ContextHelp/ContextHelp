# Notification Plugin (design example, not buildable)

This page described a notification plugin with its own JSON state under `~/.contexthelp/plugins/notifications/`, CLI/email/webhook channels and routing rules. None of it can run today:

- No plugin is loaded at runtime, and the `plugins:` config key is read by no code ([plugins.md](plugins.md#current-state)).
- The plugin API (`pkg/pluginapi`) has no plugin storage directory, no channel or delivery hooks, and no way to print on the next CLI call.

## What exists instead

| Need | Today |
|------|-------|
| Be reminded about an object | `ctxt remind set <id> <time-expression>`, `ctxt remind list`; `dpkms serve --reminder-interval` checks due reminders |
| Alerts on auth failures / ACL denials | `security.alerts` (webhook or SMTP) — [security/model/controls.md](../security/model/controls.md#audit-log-and-alerts) |
| Live object events | `GET /api/v1/events` (SSE) |
| Failed jobs | `dpkms job list --state failed` |

## Building a plugin

Start with [plugins.md](plugins.md) and the [plugin authoring guide](../plugin-authoring-guide.md). The extension points are `Plugin`, `PipelineStep`, `PostIngestHook`, `AliasResolver`, `OutputGenerator` and `PluginRegistryProvider`.

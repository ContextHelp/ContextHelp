# Notification Plugin (Optional Core Plugin)

The **Notification Plugin** provides a flexible, configurable system for generating, routing, and delivering user notifications. It is implemented entirely as an optional plugin that does **not require any changes to the core system**, relying instead on existing extensibility mechanisms (plugin storage, plugin hooks, CLI/REST injection, JSON-based state).

Notifications can be triggered by plugin-managed events such as price drops, refresh failures, feed updates, or any domain-specific condition defined inside the plugin. They do not require core involvement.

---

## Overview

The Notification Plugin enables ContextHelp to:

- Emit user-facing notifications of various message types
- Route notifications to multiple channels (CLI, email, webhook, etc.)
- Allow multiple channels per level
- Store pending and historical notifications in plugin-owned JSON
- Display pending alerts on next CLI/REST interaction
- Offer fine-grained control over message routing
- Act entirely independently of core, as an add-on module

The plugin is designed for:

- price monitoring plugins
- feed update alerts
- pipeline error notifications
- periodic status summaries
- custom workflows

---

## Goals

- Provide a multi-channel notification system entirely in plugin space
- Avoid any required changes to core storage, schema, or pipelines
- Consume only what plugin APIs already expose
- Demonstrate how plugins can extend user experience holistically
- Offer structured delivery without modifying CLI/REST/gRPC implementations

---

## Architecture Overview

The plugin is composed of:

1. **Notification Manager**
   - Handles creation, routing, formatting, and persistence.

2. **Plugin JSON Storage**
   - Maintains state under plugin’s directory:
     `~/.contexthelp/plugins/notifications/state.json`

3. **Channel Handlers**
   - CLI (prints on next invocation)
   - Webhooks
   - Email adapters (simple SMTP config)
   - Local logs
   - Future plugin-defined channels

4. **Routing Engine**
   - Determines which channel(s) should receive each message level/type.

5. **Pending Queue / History Store**
   - Maintains unread messages
   - Provides API to mark notifications delivered

6. **Hook Points**
   - Plugins trigger notifications through the Notification Plugin’s API

Since the Notification Plugin is itself a plugin, it listens only to events that *the plugin itself declares* (e.g., via helper functions like `notifications.Create()`).

---

## Storage Model

The plugin stores its entire state in JSON:

```
{
  "channels": {
    "cli": [
      { "levels": ["info","warning","error"] }
    ],
    "email": [
      {
        "address": "user@example.com",
        "levels": ["error"]
      }
    ],
    "webhook": [
      {
        "url": "https://hooks.example.com/context",
        "levels": ["info","update"]
      }
    ]
  },
  "pending": [
    {
      "id": "uuid",
      "timestamp": 1710000000,
      "type": "price_drop",
      "level": "warning",
      "payload": { "product": "Laptop", "old": 1299.99, "new": 1199.00 },
      "read": false
    }
  ],
  "history": [],
  "rules": [
    {
      "type_match": "price_drop",
      "levels": ["warning","error"],
      "channels": ["cli","webhook"]
    }
  ],
  "metadata": {
    "last_cleanup": 1710000000
  }
}
```

This requires **no changes** to the core storage layer.

---

## Configuration Model

### Global Plugin Configuration

```
notifications:
  enabled: true
  max_history: 5000
  cleanup_interval_seconds: 86400
  channels:
    cli:
      - levels: ["info", "warning", "error"]
    email:
      - address: "user@example.com"
        levels: ["error"]
    webhook:
      - url: "https://hooks.example.com/context"
        levels: ["update", "info"]
  rules:
    - type_match: "price_drop"
      channels: ["cli", "email"]
```

### Bookmark-Level Customization (optional)

Plugins may attach notification preferences to bookmarks they control:

```
"notifications": {
  "suppress": false,
  "custom_channels": ["cli", "webhook"]
}
```

This is handled entirely in plugin space.

---

## Notification API (Plugin-only)

The Notification Plugin exposes this internal API to *other plugins*:

### Create Notification

```
notifications.Create(type, level, payload)
```

### Mark As Read

```
notifications.MarkRead(id)
```

### Get Pending

```
notifications.GetPending()
```

### Clear History

```
notifications.ClearHistory()
```

No core code needs to be aware of these operations.

---

## Delivery Channels

Each channel is implemented as a simple handler inside the plugin:

### 1. CLI Channel

Injected into CLI command output by wrapping the CLI’s top-level handler.

Shows all unread notifications that match CLI levels.

### 2. Email Channel

Minimal SMTP sender:

```
host: smtp.example.com
port: 587
username: ...
password: ...
```

Used only if configured.

### 3. Webhook Channel

POST JSON payload to configured endpoint.

### 4. Log File Channel

Append notifications to `notifications.log`.

### 5. Custom (Plugin-defined) Channels

Users can add arbitrary custom transports via config.

All within plugin space.

---

## Notification Routing

Routing is purely local:

1. Determine message `type` and `level`.
2. Match rules in plugin config.
3. For each channel:
   - Check if channel accepts the level.
   - Deliver notification to channel handler.

No core involvement.

---

## Pending Queue & History

Workflow:

- New notification → saved to `pending[]`
- On CLI/REST/gRPC call → plugin surfaces pending messages
- Delivered notifications → moved to `history[]` or pruned

Again, all inside plugin space.

---

## Plugin Hooks

The plugin uses only existing generic hooks:

- `post_pipeline_step`
- `post_ingest`
- `post_refresh`
- `plugin_event(type, payload)` (plugin-defined)
- `on_cli_start`
- `on_rest_response`

None require core changes.
All are supported by ADR-012 design philosophy.

---

## Security & Privacy

- All notifications remain fully local unless user configures outbound channels.
- Webhooks and email require explicit opt-in.
- No cross-plugin access unless allowed by user.

No additional security model needed.

---

## Example Workflow

### 1. Price Monitor Plugin detects a price drop

```
notifications.Create(
  "price_drop",
  "warning",
  { "url": "...", "old": 1299.99, "new": 1199.00 }
)
```

### 2. Notification Plugin receives it
Routes to:

- CLI (immediate next invocation)
- Webhook (immediate push)

### 3. User runs any `ch` command
CLI handler detects unread messages:

```
Price Drop Alert:
Laptop decreased from $1299.99 to $1199.00.
```

---

## Use Cases

### 1. Price Monitoring Alerts
Triggered by Price Monitor Plugin.

### 2. Feed Updates
RSS Feed Plugin informs user about new items.

### 3. Job Failures
Pipeline failure alerts by custom plugin logic.

### 4. Refresh Failures
Useful for network monitoring.

### 5. Entity or Mention Conflicts
For semantic consistency plugins.

---

## Summary

The Notification Plugin is a fully self-contained module that:

- Works entirely with plugin storage and plugin hooks
- Requires **no modifications** to core architecture
- Routes messages to multiple channels
- Stores notifications in JSON
- Displays user alerts without dependent APIs
- Provides a canonical example of a rich plugin ecosystem capability

It demonstrates how ContextHelp can offer mature, user-friendly UX enhancements while keeping the core architecture stable and minimal.
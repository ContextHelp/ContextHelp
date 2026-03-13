# Event System (CloudEvents)

dPKMS and `ctxt` use a unified event system based on the **CloudEvents v1.0** specification. This enables asynchronous communication, plugin integration, and auditability across the substrate and the brain.

---

## Overview

The event system consists of:
1. **CloudEvents Spec:** A standardized format for event metadata.
2. **Event Bus:** A central interface for publishing and subscribing to events.
3. **Local Bus:** An in-memory implementation for single-node deployments.

---

## Event Format (CloudEvents v1.0)

Every event emitted by the system follows this structure:

```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "source": "service.analyze",
  "specversion": "1.0",
  "type": "job.enqueued",
  "datacontenttype": "application/json",
  "time": "2026-03-13T08:30:00Z",
  "data": { ... }
}
```

| Field | Description |
| :--- | :--- |
| `id` | Unique identifier for the event (UUID v4). |
| `source` | Identifies the context in which an event happened (e.g., `service.analyze`, `worker.pool`). |
| `specversion` | The version of the CloudEvents specification (fixed at `1.0`). |
| `type` | Describes the type of event (e.g., `job.enqueued`, `job.completed`). |
| `datacontenttype` | Content type of the `data` value (typically `application/json`). |
| `time` | Timestamp of when the occurrence happened (UTC). |
| `data` | The event-specific payload. |

---

## Internal Event Bus

The `internal/events` package provides the `Bus` interface:

```go
type Bus interface {
    Publish(ctx context.Context, e Event) error
    Subscribe(eventType string, handler Handler)
    Close() error
}
```

### Usage

**Publishing an event:**

```go
ev, _ := events.NewEvent("my.source", "my.event.type", payload)
bus.Publish(ctx, ev)
```

**Subscribing to events:**

```go
bus.Subscribe("job.completed", func(ctx context.Context, e Event) error {
    // Handle job completion
    return nil
})

// Catch-all subscription
bus.Subscribe("*", func(ctx context.Context, e Event) error {
    // Log all events
    return nil
})
```

---

## Catalog of Events

### Job Events
Emitted by the service layer and worker pool.

| Event Type | Source | Payload | Trigger |
| :--- | :--- | :--- | :--- |
| `job.enqueued` | `service.analyze` \| `worker.pool.fanout` | `storage.Job` | A new job is added to the queue (direct analyze or fan-out). |
| `job.completed` | `worker.pool` | `{"job_id": "...", "result_id": "..."}` | A job finished successfully. |
| `job.failed` | `worker.pool` | `{"job_id": "...", "error": "..."}` | A job failed after retries. |

### Object Events
Emitted by the service layer when objects are manually managed.

| Event Type | Source | Payload | Trigger |
| :--- | :--- | :--- | :--- |
| `object.updated` | `service.objects` | `{"id": "..."}` | An existing knowledge object is updated. |
| `object.deleted` | `service.objects` | `{"id": "..."}` | A knowledge object is deleted. |

---

## Future Extensions

- **Distributed Bus:** Implementations for Redis or NATS for multi-node deployments.
- **Webhook Bridge:** A plugin that forwards internal CloudEvents to external webhooks.
- **Audit Log Store:** A subscriber that persists all security-relevant events to a tamper-evident table.

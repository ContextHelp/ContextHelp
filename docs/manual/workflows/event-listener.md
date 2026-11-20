# Workflow: Listen for Events

## Goal

React to objects as ctxt ingests them from an external program, without polling `GET /objects`.

## Prerequisites

- An owner API key (the stream is owner-only today; a non-owner key gets 403 `NOT_SHARED`). A private instance needs none.
- The instance URL, for example `http://127.0.0.1:8080`.

## Quick start

```bash
curl -N -H "Authorization: Bearer $CTXT_TOKEN" http://127.0.0.1:8080/api/v1/events
```

Each event is one `data:` line holding a CloudEvents JSON document:

```text
data: {"source":"worker.pool","specversion":"1.0","type":"ctxt.runtime.object.ingested","data":{"object_id":"852e0fac-…","type":"text","source":"yta","source_key":"","pipeline":"text.short","tags":["star-candidate"],"profile_id":"","duration_ms":173}}
```

Filter in the listener, not the server: keep `type == "ctxt.runtime.object.ingested"` and match `data.source` (and `data.source_key`, when set). Fetch the object with `GET /api/v1/objects/{object_id}` for its content. Topics and fields: [REST API, Events](../../api/api-rest.md#events).

## Catch up after a gap

The stream has no event ids, no resume, no heartbeat and no topic filter; events published while the listener is down are gone. Pair the stream with a cursor:

```bash
ctxt list --cursor my-listener --advance --tagged star-candidate
```

It returns the objects created since the cursor's last advance. Run it at listener start-up, then follow the stream.

## Output validation

- The stream sends headers immediately; `curl -N` shows no output until an event happens.
- Ingest a test object (`ctxt analyze`) and expect one `object.ingested` with your `source`.

## Common failure modes

| Symptom | Cause |
|---|---|
| 401 | No or invalid key on a protected instance |
| 403 `NOT_SHARED` | Key belongs to a non-owner principal |
| Event missing | Disconnected at the time, per-connection buffer full, or the write path publishes no event (`ctxt ingest`) |

## Durable delivery

An at-least-once outbox for external listeners is designed in [ADR-075](../../decisions/ADR-075-external-hook-surface.md).

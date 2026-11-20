# Workflow: Listen for Events

## Goal

React to objects as ctxt ingests them from an external program, without polling `GET /objects`.

## Prerequisites

- An API key with `read:objects`. A non-owner key listens too: it gets only the object events for objects its share grant lets it read (see [Non-owner listeners](#non-owner-listeners)). The owner's key gets every event. A private instance needs none.
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

## Non-owner listeners

A listener should hold a read-only key with a narrow grant, not an owner key.

- It receives `object.ingested`, `object.updated`, `object.deleted` and `object.raw_stored` for objects its grant reads, and nothing else. Jobs and the rest stay owner-only.
- The payload matches what a read of the object shows. `object.updated` has no `fields` list. `source_key` is present when the grant's policy projects `source`, and absent otherwise. When a payload lacks what the listener needs, fetch the object with `GET /api/v1/objects/{object_id}`.
- A key with no grant gets `200` and a stream that carries only heartbeats. That is not an error: the grant is empty.
- `object.deleted` arrives only for an object the same connection was already told about. After a reconnect, or for an object that predates the connection, run the catch-up below and treat objects that no longer appear as deleted.
- A grant change takes effect when the listener reconnects.

## Heartbeat

An idle stream writes the comment line `: heartbeat` every 15 seconds (`server.events_heartbeat`). SSE clients ignore comment lines. A listener can treat a long silence (a few intervals with no line at all) as a dead connection and reconnect.

## Catch up after a gap

The stream has no event ids, no resume and no topic filter; events published while the listener is down are gone. Pair the stream with a cursor:

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
| Stream stays empty (heartbeats only) | The key's principal has no grant, or none that reads the objects you expect |
| Event missing | Disconnected at the time, per-connection buffer full, or the write path publishes no event (`ctxt ingest`) |

## Durable delivery

An at-least-once outbox for external listeners, with monotonic event ids and `Last-Event-ID` resume inside a retention window, is designed in [ADR-075](../../decisions/ADR-075-external-hook-surface.md).
